// Package freeradius is a simple Free Radius management package, which allows you to manage Free Radius servers.
package radius

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path"
	"time"

	"github.com/apex/log"
	"github.com/centrifugal/centrifuge-go"
	"github.com/docker/go-connections/nat"
	"github.com/justinas/alice"
	"github.com/timshannon/bolthold"

	dockerclient "github.com/docker/docker/client"

	"github.com/node-isp/node-isp/pkg/service"
)

var freeradiusImage = "ghcr.io/node-isp/freeradius:4.0"

type Radius struct {
	client     *centrifuge.Client
	mux        *http.ServeMux
	middleware alice.Chain

	mgr *service.Manager

	// ApiURL is the URL of the Node ISP Websocket endpoint
	apiUrl string

	// RadiusServerSecret is the secret used to authenticate with the Radius server.
	radiusServerSecret string

	// routerManager is the router manager.
	routerManager *routerManager

	// serviceManager is the service manager.
	serviceManager *serviceManager
}

// Run runs the Radius server.
func Run(
	apiUrl, radiusServerSecret, storageDir string,
) error {
	log.Infof("🚀 Starting Radius server 🚀")

	headers := map[string][]string{
		"Authorization": {"Bearer " + radiusServerSecret},
	}

	// create the cache dir if it doesn't exist
	if err := os.MkdirAll(storageDir, 0755); err != nil {
		return err
	}

	log.WithField("backend", apiUrl).Infof("connecting to backend")

	client := centrifuge.NewProtobufClient(
		apiUrl,
		centrifuge.Config{Header: headers},
	)
	defer client.Close()

	cache, err := bolthold.Open(path.Join(storageDir, "radius.db"), 0666, nil)
	if err != nil {
		return err
	}

	defer cache.Close()

	// Create a docker client
	docker, err := dockerclient.NewClientWithOpts(dockerclient.FromEnv, dockerclient.WithAPIVersionNegotiation())
	if err != nil {
		log.WithError(err).Fatal("Failed to create docker client")
	}

	f := &Radius{
		client:             client,
		mux:                http.NewServeMux(),
		apiUrl:             apiUrl,
		radiusServerSecret: radiusServerSecret,
		routerManager: &routerManager{
			cache: cache,
		},
		serviceManager: &serviceManager{
			cache: cache,
		},
	}

	// Run the routes.
	f.routes()

	client.OnConnected(f.realtimeConnected)
	client.OnPublication(f.realtimePublication)
	client.OnMessage(f.realtimeMessage)

	// If the backend is down, we can use the cached data to continue to serve requests.
	// Reconnects are handled by the client itself.
	if err := client.Connect(); err != nil {
		log.WithError(err).Error("failed to connect to backend, will continue to serve cached data")
	}

	httpListening := make(chan bool, 1)

	// Start the HTTP server in a goroutine.
	go func() {
		close(httpListening)
		err := http.ListenAndServe(":9999", f.mux)
		if err != nil {
			log.WithError(err).Fatal("failed to start HTTP server")
		}
	}()

	<-httpListening

	logDir := path.Join(storageDir, "logs")
	err = os.MkdirAll(logDir, 0755)
	if err != nil {
		log.WithError(err).Fatal("failed to create log directory")
	}

	// Create a service manager, and boot freeradius
	mgr := service.New(
		docker,
		log.WithField("component", "service"),
		logDir,
	)

	f.mgr = mgr

	// Boot freeradius
	freeradiusSvc := &service.Service{
		Name:  "freeradius",
		Image: freeradiusImage,
		Env: []string{
			fmt.Sprintf("FREERADIUS_API_URL=%s", "http://192.168.15.100:9999"),
		},
		PortBindings: map[nat.Port][]nat.PortBinding{
			"1812/tcp": {{HostIP: "0.0.0.0", HostPort: "1812"}},
			"1812/udp": {{HostIP: "0.0.0.0", HostPort: "1812"}},
			"1813/tcp": {{HostIP: "0.0.0.0", HostPort: "1813"}},
			"1813/udp": {{HostIP: "0.0.0.0", HostPort: "1813"}},
		},
		ExposedPorts: map[nat.Port]struct{}{
			"1812/tcp": {},
			"1812/udp": {},
			"1813/tcp": {},
			"1813/udp": {},
		},
	}

	if err := mgr.EnsureService(context.Background(), freeradiusSvc); err != nil {
		log.WithError(err).Fatal("failed to add freeradius service")
	}

	// Wait for the service to be ready
	select {}
}

func (f *Radius) realtimeConnected(e centrifuge.ConnectedEvent) {
	log.WithField("client_id", e.ClientID).Info("backend connected")
	go func() {
		defer log.Info("realtime loop exited")

		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()

		for ; true; <-ticker.C {

			// Get the state, and check if we're connected.
			state := f.client.State()

			if state != centrifuge.StateConnected {
				log.WithField("state", state).Warn("client not connected, refusing to update")
				continue
			}

			log.Info("updating data from backend")
			go f.loadRoutersFromAPI()
			go f.loadServicesFromAPI()
		}
	}()
}

func (f *Radius) realtimePublication(e centrifuge.ServerPublicationEvent) {
	log.
		WithField("data", string(e.Data)).
		Info("publication")
}

func (f *Radius) realtimeMessage(event centrifuge.MessageEvent) {
	log.
		WithField("data", string(event.Data)).
		Info("message")
}
