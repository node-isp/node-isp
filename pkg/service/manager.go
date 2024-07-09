package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/NYTimes/logrotate"
	"github.com/apex/log"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
)

type Manager struct {
	mu sync.Mutex

	// d is the docker client
	d *client.Client

	Logdir string `json:"logdir"`
	log    *log.Entry

	// Network is the ID of the Network that the Services run on
	Network string `json:"network"`

	// Services is a list of Services that the manager manages
	Services map[string]*Service `json:"services"`
}

// New creates a new service manager
func New(docker *client.Client, log *log.Entry, logdir string) *Manager {
	manager := &Manager{
		d:        docker,
		log:      log,
		Logdir:   logdir,
		Services: map[string]*Service{},
	}

	// ensure Network exists
	netList, err := docker.NetworkList(context.Background(), network.ListOptions{Filters: filters.NewArgs(filters.Arg("label", "app=nodeisp"))})
	if err != nil {
		log.WithError(err).Fatal("Failed to list networks")
	}

	if len(netList) > 0 {
		manager.Network = netList[0].ID
		log.Infof("using Network %s", manager.Network)
		return manager
	}

	net, err := docker.NetworkCreate(context.Background(), "nodeisp", network.CreateOptions{
		Labels: map[string]string{
			"app": "nodeisp",
		},
	})
	if err != nil {
		log.WithError(err).Fatal("Failed to create Network")
	}

	log.Infof("created Network %s", net.ID)

	manager.Network = net.ID
	return manager
}

// EnsureService adds a service to the manager if it does not exist, and starts it, waiting for it to be healthy
func (m *Manager) EnsureService(ctx context.Context, s *Service) error {
	s.logfile = filepath.Join(m.Logdir, s.Name+".log")

	w, err := logrotate.NewFile(s.logfile)
	if err != nil {
		m.log.WithError(err).Fatal("Failed to create log file")
	}

	s.w = w
	s.log = m.log.WithField("service", s.GetName())

	m.mu.Lock()
	m.Services[s.Name] = s
	m.mu.Unlock()

	return m.ensureRunning(ctx, s.Name)
}

// ListContainers lists all containers from the manager
func (m *Manager) ListContainers(ctx context.Context) ([]types.Container, error) {
	return m.d.ContainerList(ctx, container.ListOptions{All: true, Filters: filters.NewArgs(
		filters.Arg("label", "app=nodeisp"),
	)})
}

func (m *Manager) ensureRunning(ctx context.Context, name string) error {
	svc, ok := m.Services[name]
	if !ok {
		return errors.New("service not found")
	}

	ctx, cancel := context.WithTimeout(ctx, 180*time.Second)
	defer cancel()

	// Find the container, if it exists
	containers, err := m.d.ContainerList(ctx, container.ListOptions{All: true, Filters: filters.NewArgs(
		filters.Arg("label", "app=nodeisp"),
		filters.Arg("label", "service="+svc.Name),
	)})
	if err != nil {
		return err
	}

	var c *types.Container

	for _, ctr := range containers {
		// Does the hash match?
		hash := ctr.Labels["hash"]
		if hash == svc.GetHash() {
			c = &ctr
			break
		}
	}

	if c != nil {
		svc.log.WithField("container", c.ID).WithField("state", c.State).Info("found existing container")
	}

	// Delete any old containers, if they exist
	for _, ctr := range containers {
		// As long as the container is not the one we want to keep, delete it

		if c == nil || ctr.ID != c.ID {
			// If the container is running, stop it
			if ctr.State == "running" {
				if err := m.d.ContainerStop(ctx, ctr.ID, container.StopOptions{}); err != nil {
					svc.log.WithError(err).Error("failed to stop container")
				}
			}

			// Remove the container
			if err := m.d.ContainerRemove(ctx, ctr.ID, container.RemoveOptions{Force: true}); err != nil {
				svc.log.WithError(err).Error("failed to remove container")
			}

			svc.log.Info("removed old container")
		}
	}

	if c == nil {
		svc.log.Infof("pulling image %s", svc.Image)

		// pull the image
		pullOptions := image.PullOptions{}
		var platformOptions *v1.Platform

		if svc.Name == "app" {
			pullOptions.Platform = "linux/amd64"
			platformOptions = &v1.Platform{
				OS:           "linux",
				Architecture: "amd64",
			}
		}

		reader, err := m.d.ImagePull(ctx, svc.Image, pullOptions)
		ignorePullErrors := false
		if err != nil {
			svc.log.WithError(err).Error("failed to pull image")
			ignorePullErrors = true
		}

		if !ignorePullErrors {
			_, _ = io.Copy(os.Stdout, reader)
			_ = reader.Close()
		}
		svc.log.Info("creating container")

		resp, err := m.d.ContainerCreate(ctx, &container.Config{
			Tty:        true,
			Image:      svc.Image,
			Env:        svc.Env,
			Entrypoint: svc.Entrypoint,
			Labels: map[string]string{
				"app":     "nodeisp",
				"service": svc.Name,
				"hash":    svc.GetHash(),
			},
			ExposedPorts: svc.ExposedPorts,
		}, &container.HostConfig{
			NetworkMode: container.NetworkMode(m.Network),
			Mounts:      svc.Mounts,
			RestartPolicy: container.RestartPolicy{
				Name: container.RestartPolicyUnlessStopped,
			},
			PortBindings: svc.PortBindings,
		}, &network.NetworkingConfig{}, platformOptions, svc.GetName())

		if err != nil {
			panic(err)
		}

		c = &types.Container{ID: resp.ID, State: "created"}
	}

	// Start the container if it's not running
	if c.State != "running" {
		svc.log.Infof("starting container")
		if err := m.d.ContainerStart(ctx, c.ID, container.StartOptions{}); err != nil {
			return err
		}
	}

	svc.statusChan, svc.errChan = m.d.ContainerWait(ctx, c.ID, container.WaitConditionNotRunning)
	svc.output, err = m.d.ContainerAttach(ctx, c.ID, container.AttachOptions{
		Stream: true,
		Stdout: true,
		Stderr: true,
		Logs:   true,
	})

	// Start a reader for the output, and log it to debug mode and the service log file
	go func() {
		// read the output line by line
		if svc.output.Reader == nil {
			svc.log.Error("failed to read output")
			return
		}

		for {
			buf := make([]byte, 1024)
			_, err := svc.output.Reader.Read(buf)
			if err != nil {
				svc.log.WithError(err).Error("failed to read output")
				break
			}

			// Split the buffer into lines
			// Strip any trailing newline or timestamp from the line
			line := bytes.Trim(buf, "\x00")

			// Trim any whitespace from the line
			line = bytes.Trim(line, " ")

			// Trim any newlines from the line
			line = bytes.Trim(line, "\n")

			// If the line is not empty, log it
			if len(line) >= 0 {
				svc.log.Debug(string(line))
				_, _ = svc.w.WriteString(string(line))
			}
		}
	}()

	return nil
}

func (m *Manager) RunCommand(ctx context.Context, server *Service, cmd []string) error {
	server.log.WithField("command", cmd).Info("running command")

	exec, err := m.d.ContainerExecCreate(ctx, server.GetName(), container.ExecOptions{
		Cmd:          cmd,
		AttachStdout: true,
		AttachStderr: true,
		AttachStdin:  true,
		Tty:          true,
	})

	if err != nil {
		return err
	}

	resp, err := m.d.ContainerExecAttach(ctx, exec.ID, container.ExecAttachOptions{})
	if err != nil {
		return err
	}

	// Read the output
	go func() {
		buf := new(strings.Builder)
		_, err := io.Copy(buf, resp.Reader)
		if err != nil {
			server.log.WithError(err).Error("failed to read output")
			return
		}

		// Split the buffer into lines,
		for _, line := range strings.Split(buf.String(), "\n") {
			line = strings.TrimSpace(line)
			if line != "" && line != "\n" && line != " " {
				server.log.WithField("command", cmd).Debug(line)
			}
		}
	}()

	return nil
}
