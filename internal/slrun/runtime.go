package slrun

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	"github.com/marcorentap/slrun/internal/policy"
	"github.com/marcorentap/slrun/internal/types"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

type Runtime struct {
	functions []*types.Function
	running   bool
	cli       *client.Client // Docker client
	policy    types.Policy
	tickRate  time.Duration
}

func NewRuntime(functions []*types.Function, policyId types.PolicyID) (*Runtime, error) {
	dockerCli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}

	r := Runtime{
		functions: functions,
		running:   false,
		cli:       dockerCli,
		tickRate:  5 * time.Millisecond,
	}

	var pol types.Policy
	switch policyId {
	case types.AlwaysColdPolicy:
		pol = &policy.AlwaysCold{
			Funcs:     functions,
			StartFunc: r.startFunction,
			StopFunc:  r.stopFunction,
		}
	case types.AlwaysHotPolicy:
		pol = &policy.AlwaysHot{
			Funcs:     functions,
			StartFunc: r.startFunction,
			StopFunc:  r.stopFunction,
		}

	default:
		return nil, fmt.Errorf("unknown policy ID: %d", policyId)
	}

	r.policy = pol

	return &r, nil
}

func (r *Runtime) startFunction(function *types.Function) error {
	ctx := context.Background()
	config := &container.Config{
		Image: function.ImageName,
	}
	networkingConfig := &network.NetworkingConfig{}
	platform := &ocispec.Platform{}

	port, err := nat.NewPort("tcp", "80")
	if err != nil {
		return err
	}
	portMap := nat.PortMap{}
	portMap[port] = []nat.PortBinding{
		{
			HostIP:   "127.0.0.1", // Functions are directly accessible only on localhost
			HostPort: "",          // Allocate a random port
		},
	}
	hostConfig := &container.HostConfig{
		PortBindings: portMap,
	}

	resp, err := r.cli.ContainerCreate(ctx, config, hostConfig, networkingConfig, platform, "")
	if err != nil {
		return err
	}

	// Start container, then set function metadata
	startOptions := container.StartOptions{}
	err = r.cli.ContainerStart(ctx, resp.ID, startOptions)
	if err != nil {
		return err
	}

	inspResp, err := r.cli.ContainerInspect(ctx, resp.ID)
	if err != nil {
		return err
	}

	hostPort := inspResp.NetworkSettings.Ports["80/tcp"][0].HostPort
	function.ContainerId = resp.ID
	function.Port, _ = strconv.Atoi(hostPort)
	function.IsRunning = true
	return nil
}

func (r *Runtime) stopFunction(function *types.Function) error {
	ctx := context.Background()
	stopTimeout := 0 // Don't wait for graceful shutdown
	err := r.cli.ContainerStop(ctx, function.ContainerId, container.StopOptions{
		Timeout: &stopTimeout,
	})
	if err != nil {
		return err
	}
	function.IsRunning = false
	return nil
}

func (r *Runtime) clearFunctionContainers() error {
	ctx := context.Background()
	summary, err := r.cli.ContainerList(ctx, container.ListOptions{})
	if err != nil {
		return err
	}

	stopTimeout := 0 // Don't wait for graceful shutdown
	for _, fun := range r.functions {
		// Check container state
		for _, summ := range summary {
			if summ.Image == fun.ImageName {
				err := r.cli.ContainerStop(ctx, summ.ID, container.StopOptions{
					Timeout: &stopTimeout,
				})
				if err != nil {
					return err
				}

				log.Printf("Stopped existing container %v\n", summ.Names)
			}
		}
	}

	return nil
}

func (r *Runtime) callFunction(function *types.Function, path string, prevReq *http.Request) ([]byte, error) {
	err := r.policy.PreFunctionCall(function)
	if err != nil {
		return nil, err
	}

	for {
		resp, err := http.Head("http://127.0.0.1:" + strconv.Itoa(function.Port))
		if err == nil {
			resp.Body.Close()
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	url := "http://127.0.0.1:" + strconv.Itoa(function.Port) + path
	req, err := http.NewRequest(prevReq.Method, url, nil)

	if err != nil {
		return nil, err
	}

	req.Header = prevReq.Header
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("Error calling function %v: %v", function.Name, err)
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Cannot read function %v response: %v\n", function.Name, err)
		return nil, err
	}

	err = r.policy.PostFunctionCall(function)
	if err != nil {
		return nil, err
	}
	return body, nil
}

func (r *Runtime) CallFunctionByName(name string, path string, prevReq *http.Request) ([]byte, error) {
	for _, fun := range r.functions {
		if fun.Name == name {
			return r.callFunction(fun, path, prevReq)
		}
	}

	log.Printf("Unknown function requested %v\n", name)
	return nil, fmt.Errorf("function %v not found", name)
}

func (r *Runtime) Start() error {
	// Remove running containers
	err := r.clearFunctionContainers()
	if err != nil {
		return err
	}

	for _, fun := range r.functions {
		if fun.IsRunning {
			log.Printf("Stopping function %v\n", fun.Name)
			err = r.stopFunction(fun)
			log.Printf("Stopped function %v\n", fun.Name)
			if err != nil {
				return err
			}
		}
	}

	err = r.policy.OnRuntimeStart()
	if err != nil {
		return err
	}

	go func() {
		for {
			time.Sleep(r.tickRate)

			err = r.policy.OnTick()
			if err != nil {
				log.Printf("Error on tick: %v\n", err)
			}
		}
	}()

	return nil
}

func (r *Runtime) Stop() error {
	// Stop function containers
	for _, fun := range r.functions {
		log.Printf("Stopping function %v container %v\n", fun.Name, fun.ContainerId)
		err := r.stopFunction(fun)
		if err != nil {
			log.Printf("Cannot stop function %v: %v\n", fun.Name, err)
			return err
		}
		log.Printf("Stopped function %v\n", fun.Name)
	}
	return nil
}
