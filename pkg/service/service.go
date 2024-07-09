package service

import (
	"crypto/md5"
	"fmt"
	"sync"

	"github.com/NYTimes/logrotate"
	"github.com/apex/log"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/go-connections/nat"
)

// Service is a docker container, that runs part of the application
type Service struct {
	logfile string
	mu      sync.Mutex
	log     *log.Entry
	w       *logrotate.File

	hash string

	statusChan <-chan container.WaitResponse
	errChan    <-chan error

	output types.HijackedResponse

	// Name is the name of the service
	Name string `json:"name"`

	// Image is the docker image that the service runs
	Image string `json:"image"`

	// Mounts is a list of volumes that the service mounts
	Mounts []mount.Mount `json:"mounts"`

	// Env is a list of environment variables that the service uses
	Env []string `json:"env"`

	// Ports is a list of ports that the service exposes
	PortBindings map[nat.Port][]nat.PortBinding `json:"port_bindings"`

	// ExposedPorts is a list of ports that the service exposes
	ExposedPorts nat.PortSet `json:"exposed_ports"`

	// Command is the command that the service runs
	Entrypoint []string `json:"entrypoint"`
}

// GetName returns the name of the service
func (s *Service) GetName() string {
	hash := s.GetHash()

	// Return a unique name for the service
	return fmt.Sprintf("nodeisp_%s_%s", s.Name, hash[:8])
}

// GetHash returns a hash of the service configuration
func (s *Service) GetHash() string {
	// If we've computed the hash before, return it
	if s.hash != "" {
		return s.hash
	}

	// Lock the mutex, so we don't compute the hash multiple times
	s.mu.Lock()
	defer s.mu.Unlock()

	// Encode the environment variables to JSON, and md5 hash them;
	// this is a simple way to get a unique hash for the service that changes when the configuration changes
	// which will force a container restart later
	s.hash = fmt.Sprintf("%x", md5.Sum([]byte(fmt.Sprintf("%s %v %v %v %s", s.Image, s.Env, s.Mounts, s.PortBindings, s.Entrypoint))))

	return s.hash
}
