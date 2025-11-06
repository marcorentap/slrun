package slrun

import (
	"context"
	"log"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

type Runtime struct {
	functions []*Function
	running   bool
	cli       *client.Client // Docker client
}

func NewRuntime(functions []*Function) (*Runtime, error) {
	dockerCli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}

	return &Runtime{
		functions: functions,
		running:   false,
		cli:       dockerCli,
	}, nil
}

func (r *Runtime) startFunction(function *Function) error {
	ctx := context.Background()
	config := &container.Config{
		Image: function.imageName,
	}
	hostConfig := &container.HostConfig{}
	networkingConfig := &network.NetworkingConfig{}
	platform := &ocispec.Platform{}
	resp, err := r.cli.ContainerCreate(ctx, config, hostConfig, networkingConfig, platform, "")
	if err != nil {
		return err
	}
	startOptions := container.StartOptions{}
	err = r.cli.ContainerStart(ctx, resp.ID, startOptions)
	if err != nil {
		return err
	}

	function.containerId = resp.ID
	return nil
}

func (r *Runtime) stopFunction(function *Function) error {
	ctx := context.Background()
	stopTimeout := 0 // Don't wait for graceful shutdown
	err := r.cli.ContainerStop(ctx, function.containerId, container.StopOptions{
		Timeout: &stopTimeout,
	})
	if err != nil {
		return err
	}
	return nil
}

func (r *Runtime) updateFunctionStatus() error {
	ctx := context.Background()
	summary, err := r.cli.ContainerList(ctx, container.ListOptions{})
	if err != nil {
		return err
	}

	for _, fun := range r.functions {
		// Check container state
		for _, summ := range summary {
			if summ.Image == fun.imageName {
				fun.containerId = summ.ID
				fun.running = true
			}
		}

		if fun.running {
			log.Printf("Image %v is running as %v\n", fun.imageName, fun.containerId)
		} else {
			log.Printf("Image %v is not running\n", fun.imageName)
		}
	}

	return nil
}

func (r *Runtime) CallFunction(function *Function) {

}

func (r *Runtime) Start() error {
	// Check whether functions are running
	err := r.updateFunctionStatus()
	if err != nil {
		return err
	}

	// Remove running containers
	for _, fun := range r.functions {
		if fun.running {
			log.Printf("Stopping function %v\n", fun.Name)
			err = r.stopFunction(fun)
			log.Printf("Stopped function %v\n", fun.Name)
			if err != nil {
				return err
			}
		}
	}

	// Start function containers
	for _, fun := range r.functions {
		log.Printf("Starting function %v\n", fun.Name)
		err = r.startFunction(fun)
		if err != nil {
			log.Printf("Cannot start function %v: %v\n", fun.Name, err)
			return err
		}
		log.Printf("Started function %v as container %v\n", fun.Name, fun.containerId)
	}

	return nil
}

func (r *Runtime) Stop() error {
	// Stop function containers
	for _, fun := range r.functions {
		log.Printf("Stopping function %v container %v\n", fun.Name, fun.containerId)
		err := r.stopFunction(fun)
		if err != nil {
			log.Printf("Cannot stop function %v: %v\n", fun.Name, err)
			return err
		}
		log.Printf("Stopped function %v\n", fun.Name)
	}
	return nil
}
