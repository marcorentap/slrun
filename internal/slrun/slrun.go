package slrun

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"archive/tar"
	"bytes"
	"io"
	"path/filepath"

	"github.com/docker/docker/api/types/build"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/marcorentap/slrun/internal/types"
)

var config *types.Config
var dockerCli *client.Client
var dockerCtx context.Context
var runtime *Runtime
var host string
var port int

// createTarContext creates a tar archive of the directory at dirPath.
func createTarContext(dirPath string) (io.Reader, error) {
	buf := new(bytes.Buffer)
	tw := tar.NewWriter(buf)

	err := filepath.Walk(dirPath, func(file string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		header, err := tar.FileInfoHeader(fi, fi.Name())
		if err != nil {
			return err
		}

		// Use relative path so the archive structure matches the relative paths in the context directory
		relPath, err := filepath.Rel(dirPath, file)
		if err != nil {
			return err
		}
		header.Name = relPath

		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		if fi.Mode().IsRegular() {
			f, err := os.Open(file)
			if err != nil {
				return err
			}
			defer f.Close()

			if _, err := io.Copy(tw, f); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	if err := tw.Close(); err != nil {
		return nil, err
	}

	return buf, nil
}

func BuildFunctionImage(function *types.Function) error {
	buildCtx, err := createTarContext(function.BuildDir)
	if err != nil {
		return err
	}

	// Remove then rebuild image
	imageName := "slrun-" + function.Name
	_, err = dockerCli.ImageRemove(dockerCtx, imageName, image.RemoveOptions{
		Force:         true,
		PruneChildren: true,
	})

	if err != nil {
		// If image doesn't exist, it's ok
		if !strings.Contains(err.Error(), "No such image: slrun-") {
			return err
		}
	}

	buildResp, err := dockerCli.ImageBuild(dockerCtx, buildCtx, build.ImageBuildOptions{
		Tags: []string{imageName},
	})
	if err != nil {
		return err
	}
	defer buildResp.Body.Close()

	// We have to read from the response, else it won't build
	io.Copy(io.Discard, buildResp.Body)

	function.ImageName = imageName
	return nil
}

func Start(cfgFile string, host string, port int) error {
	// Init
	config, err := ReadConfigFile(cfgFile)
	if err != nil {
		return err
	}
	dockerCli, err = client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return err
	}
	dockerCtx = context.Background()

	// Build function images
	for _, function := range config.Functions {
		fmt.Printf("Building function image: %v => %v\n", function.Name, function.BuildDir)
		err := BuildFunctionImage(function)
		if err != nil {
			log.Printf("Cannot build image %v\n", function.ImageName)
			return err
		}

		fmt.Printf("Built function image: %v\n", function.ImageName)
	}

	// Start function manager
	log.Printf("Starting runtime\n")
	runtime, err := NewRuntime(config.Functions, config.Policy)
	if err != nil {
		return err
	}
	runtime.Start()
	fmt.Printf("Runtime started\n")

	// Start server
	listenAddr := host + ":" + strconv.Itoa(port)

	server := &http.Server{
		Addr: listenAddr,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			parts := strings.Split(r.URL.Path, "/") // /funcName/other/parts

			if len(parts) < 2 {
				return
			}

			funcName := parts[1]
			path, _ := strings.CutPrefix(r.URL.Path, "/"+funcName)

			resp, err := runtime.CallFunctionByName(funcName, path, r)
			if err != nil {
				w.Write([]byte(err.Error()))
				return
			}

			w.Write(resp)

			log.Printf("Function %v called\n", funcName)
		}),
	}
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed: %v", err)
		}
	}()
	fmt.Printf("HTTP server listening on %v\n", listenAddr)

	// Register interrupt handler
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// On interrupt...
	<-ctx.Done()
	log.Println("Received interrupt signal. Shutting down server...")

	// Shutdown server
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelShutdown()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("Cannot shutdown server. %v\n")
		return err
	}
	fmt.Printf("HTTP Server stopped\n")

	// Shutdown function manager
	runtime.Stop()
	fmt.Printf("Runtime stopped\n")

	return nil
}
