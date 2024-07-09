package server

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/NYTimes/logrotate"
	"github.com/apex/log"
	"github.com/apex/log/handlers/multi"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"

	"github.com/node-isp/node-isp/pkg/config"
	"github.com/node-isp/node-isp/pkg/database"
	"github.com/node-isp/node-isp/pkg/licence"
	"github.com/node-isp/node-isp/pkg/logger"
	"github.com/node-isp/node-isp/pkg/server/realtime"
	"github.com/node-isp/node-isp/pkg/server/webserver"
	"github.com/node-isp/node-isp/pkg/service"
	"github.com/node-isp/node-isp/pkg/updater"
)

var bakedAppRepo = "ghcr.io/node-isp/node-isp"
var bakedAppVersion = "v0.11.12"

type Server struct {
	Config *config.Config
	Log    log.Interface

	mgr *service.Manager
}

var proxyHost *url.URL

func Run() error {
	cfg, err := config.New()
	if err != nil {
		return err
	}

	// Init Logging
	p := absolutePath(filepath.Join(cfg.Storage.Logs, "/nodeisp.log"))
	w, err := logrotate.NewFile(p)
	if err != nil {
		log.WithError(err).Fatal("Failed to create log file")
	}

	log.SetLevel(log.InfoLevel)
	log.SetHandler(multi.New(logger.Default, logger.New(w.File, false)))

	log.WithField("component", "server").WithField("path", p).Info("writing log files to disk")

	srv := &Server{
		Config: cfg,
		Log:    log.WithField("component", "server"),
	}

	srv.Run()

	return nil
}

func (s *Server) Run() {
	s.Log.Info("starting Node ISP")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create a docker client
	docker, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		s.Log.WithError(err).Fatal("Failed to create docker client")
	}

	// Load the licence client, and validate it
	licenceData := absolutePath(filepath.Join(s.Config.Storage.Data, "nodeisp", "licence"))
	mkdir(licenceData)
	licenceClient, err := licence.New(s.Config.Licence.ID, s.Config.Licence.Key)

	if err != nil {
		// If a nodeisp.lic file exists within the licence directory, we can assume
		// it's been validated at-least once, and this may just be a transient error.
		// We can continue to start the server, and we'll try to validate the licence
		// again later

		// If the file does not exist, the app server will not start so there is no point in continuing
		if _, err := os.Stat(filepath.Join(licenceData, "nodeisp.lic")); os.IsNotExist(err) {
			s.Log.WithError(err).Fatal("Failed to validate licence")
		}

		s.Log.WithError(err).Error("Failed to validate licence")
	}

	// If we have a licence client, dump the file to disk
	if licenceClient != nil {
		if err := licenceClient.Store(filepath.Join(licenceData, "nodeisp.lic")); err != nil {
			s.Log.WithError(err).Fatal("Failed to store licence file")
		}
	}

	// Create a service manager, and add services
	mgr := service.New(
		docker,
		s.Log.WithField("component", "service"),
		s.Config.Storage.Logs,
	)

	s.mgr = mgr

	// Load any stored state, allowing us to use the same state between restarts instead of starting from scratch
	// If an image has been changed, it will not use the default from below this way
	if err := s.loadState(); err != nil {
		s.Log.WithError(err).Warn("failed to load state")
	}

	// Redis
	redisData := absolutePath(filepath.Join(s.Config.Storage.Data, "redis"))
	mkdir(redisData)

	var redis *service.Service

	// If we have the service already, use the image from the state
	if _, ok := mgr.Services["redis"]; ok {
		redis = mgr.Services["redis"]
	} else {
		redis = &service.Service{
			Name:  "redis",
			Image: "redis:7",
		}
	}

	// We don't care about these from the state, we just really want the image
	redis.Mounts = []mount.Mount{
		{
			Type:   mount.TypeBind,
			Source: redisData,
			Target: "/data",
		},
	}
	redis.Env = []string{
		"REDIS_PORT=6379",
		"REDIS_PASSWORD=" + s.Config.Redis.Password,
	}

	// EnsureService waits for the service to be running and fails if it can't start
	if err := mgr.EnsureService(ctx, redis); err != nil {
		s.Log.WithError(err).Fatal("Failed to start redis")
	}

	// Postgres
	postgresPort := randomFreePort()
	postgresData := absolutePath(filepath.Join(s.Config.Storage.Data, "postgres"))
	mkdir(postgresData)

	var postgres *service.Service

	// If we have the service already, use the image from the state
	if _, ok := mgr.Services["postgres"]; ok {
		postgres = mgr.Services["postgres"]
		postgresPort, err = strconv.Atoi(postgres.PortBindings["5432/tcp"][0].HostPort)
		if err != nil {
			s.Log.WithError(err).Fatal("Failed to get postgres port")
		}
	} else {
		postgres = &service.Service{
			Name:  "postgres",
			Image: "postgres:16",
		}
	}

	postgres.Mounts = []mount.Mount{
		{
			Type:   mount.TypeBind,
			Source: postgresData,
			Target: "/var/lib/postgresql/data",
		},
	}

	// Bind postgres to a random port, so we can query it from the licence client
	postgres.PortBindings = map[nat.Port][]nat.PortBinding{
		"5432/tcp": {{HostIP: "127.0.0.1", HostPort: fmt.Sprintf("%d", postgresPort)}},
	}

	postgres.Env = []string{
		"POSTGRES_USER=postgres",
		"POSTGRES_PASSWORD=" + s.Config.Database.Password,
		"POSTGRES_DB=" + s.Config.Database.Name,
	}

	if err := mgr.EnsureService(ctx, postgres); err != nil {
		s.Log.WithError(err).Fatal("Failed to start postgres")
	}

	// Gotenberg
	var gotenberg *service.Service

	// If we have the service already, use the image from the state
	if _, ok := mgr.Services["gotenberg"]; ok {
		gotenberg = mgr.Services["gotenberg"]
	} else {
		gotenberg = &service.Service{
			Name:  "gotenberg",
			Image: "getlago/lago-gotenberg:7",
		}
	}

	if err := mgr.EnsureService(ctx, gotenberg); err != nil {
		s.Log.WithError(err).Fatal("Failed to start gotenberg")
	}

	// App Server
	var appServer *service.Service

	appStorage := absolutePath(filepath.Join(s.Config.Storage.Data, "nodeisp", "storage"))
	mkdir(appStorage)

	// pick and use a random port, and bind it to the app server. We use a random port here
	// because for upgrades we can then run a new container on a new port, and toggle
	// the proxy to the new port, and then remove the old container without any downtime
	port := randomFreePort()

	if _, ok := mgr.Services["app"]; ok {
		appServer = mgr.Services["app"]
		port, err = strconv.Atoi(appServer.PortBindings["8080/tcp"][0].HostPort)
		if err != nil {
			s.Log.WithError(err).Fatal("Failed to get app server port")
		}

		// Get the app version, and set it in the updater
		ver, _ := strings.CutPrefix(appServer.Image, fmt.Sprintf("%s:", bakedAppRepo))
		updater.CurrentAppVersion = ver
	} else {
		appServer = &service.Service{
			Name:  "app",
			Image: fmt.Sprintf("%s:%s", bakedAppRepo, bakedAppVersion),
		}
		updater.CurrentAppVersion = bakedAppVersion
	}

	appDomain := s.Config.HTTPServer.Domains[0]
	appUrl := fmt.Sprintf("https://%s", appDomain)
	proxyHost, err = url.Parse(fmt.Sprintf("http://127.0.0.1:%d", port))

	appServer.Env = []string{
		"APP_VERSION=" + bakedAppVersion,
		"SERVER_NAME=:8080",
		"APP_ENV=production",
		"APP_NAME=" + s.Config.App.Name,
		"APP_KEY=" + s.Config.App.Key,
		"APP_URL=" + appUrl,

		"NODEISP_LICENCE_KEY_ID=" + s.Config.Licence.ID,
		"NODEISP_LICENCE_KEY_CODE=" + s.Config.Licence.Key,
		"NODEISP_DOMAIN=" + appDomain,

		"DB_CONNECTION=pgsql",
		"DB_HOST=" + postgres.GetName(),
		"DB_PORT=5432",
		"DB_USERNAME=postgres",
		"DB_PASSWORD=" + s.Config.Database.Password,
		"DB_DATABASE=" + s.Config.Database.Name,

		"REDIS_HOST=" + redis.GetName(),
		"REDIS_PORT=6379",

		"CACHE_DRIVER=file",
		"QUEUE_CONNECTION=redis",

		"TELESCOPE_PATH=admin/telescope",
		"HORIZON_PATH=admin/horizon",

		"FILESYSTEM_DISK=local",

		"SERVICES_GOTENBERG_URL=" + fmt.Sprintf("http://%s:3000", gotenberg.GetName()),
		"SERVICES_GOOGLE_MAPS_API_KEY=" + s.Config.Services.GoogleMapsApiKey,
	}

	appServer.Mounts = []mount.Mount{
		{
			Type:   mount.TypeBind,
			Source: licenceData,
			Target: "/etc/nodeisp/",
		},
		{
			Type:   mount.TypeBind,
			Source: appStorage,
			Target: "/app/storage/app/public",
		},
		{
			Type:   mount.TypeBind,
			Source: appStorage,
			Target: "/app/public/storage",
		},
	}

	appServer.PortBindings = map[nat.Port][]nat.PortBinding{
		"8080/tcp": {{HostIP: "127.0.0.1", HostPort: fmt.Sprintf("%d", port)}},
	}

	appServer.ExposedPorts = map[nat.Port]struct{}{
		"8080/tcp": {},
	}

	// appServer.Entrypoint = []string{"php", "artisan", "octane:start", "--host=0.0.0.0", "--port=8080"}

	if err := mgr.EnsureService(ctx, appServer); err != nil {
		s.Log.WithError(err).Fatal("Failed to start app server")
	}

	// Horizon
	worker := &service.Service{
		Name:       "horizon",
		Image:      appServer.Image,
		Env:        appServer.Env,
		Mounts:     appServer.Mounts,
		Entrypoint: []string{"/entrypoint-worker.sh"},
	}

	if err := mgr.EnsureService(ctx, worker); err != nil {
		s.Log.WithError(err).Fatal("Failed to start worker")
	}

	// Start a thread to run the crons every minute
	_ = s.storeState()

	// Run all the crons every minute, being sure not to block the cron thread
	go func() {
		for {
			go func() {
				err := s.storeState()
				if err != nil {
					s.Log.WithError(err).Error("Failed to store state")
				}
			}()
			go func() {
				if err := mgr.RunCommand(ctx, appServer, []string{"php", "artisan", "schedule:run"}); err != nil {
					s.Log.WithError(err).Error("Failed to run cron")
				}
			}()
			time.Sleep(1 * time.Minute)
		}
	}()

	db, err := database.NewDatabase("127.0.0.1", fmt.Sprintf("%d", postgresPort), "postgres", s.Config.Database.Password, s.Config.Database.Name)
	if err != nil {
		s.Log.WithError(err).Fatal("Failed to connect to database")
	}

	// Start the stats reporter
	if licenceClient != nil {
		if err := licenceClient.StartStatsReporter(db); err != nil {
			s.Log.WithError(err).Fatal("Failed to start stats reporter")
		}
	}

	// Start the HTTP and HTTPS proxy
	mux := http.NewServeMux()

	rt := realtime.RealTime{
		DB:         db,
		BackendUrl: fmt.Sprintf("%s/api/centrifugo", proxyHost),
		Log:        log.WithField("component", "realtime"),
	}

	if err := rt.Run(); err != nil {
		s.Log.WithError(err).Fatal("Failed to setup realtime server")
	}

	// Realtime server
	mux.HandleFunc("/_internal/realtime", rt.Handler)

	// Proxy to app container
	mux.Handle("/", s.setupProxy())

	if err := rt.Run(); err != nil {
		s.Log.WithError(err).Fatal("Failed to setup realtime server")
	}

	ws := webserver.New(
		mux,
		s.Config.Storage.Data,
		s.Config.HTTPServer.Domains,
		s.Config.HTTPServer.TLS.Email,
		s.Log.WithField("component", "webserver"),
	)

	go ws.Run(ctx)

	// Start the updater in the background
	u := &updater.Updater{}

	updates := make(<-chan updater.Update)
	go func() {
		updates, err = u.Start()
		if err != nil {
			s.Log.WithError(err).Error("Failed to start updater")
		}

		for {
			select {
			case update := <-updates:
				if update.Component == "app" {
					// TODO: Notify the user, and upgrade the app
				}
			}
		}
	}()

	log.WithField("component", "server").
		WithField("internalHost", proxyHost).
		WithField("AdminURL", fmt.Sprintf("https://%s/admin", s.Config.HTTPServer.Domains[0])).
		Info("Node ISP is running")

	// TODO: GRPC server for CLI, with version upgrades and stuff
	// TODO: Metrics server fun time Let the client daemon get stats, and check for updates, and push updated images

	// start GRPC server
	grpc := &grpcServer{
		docker: docker,
		mgr:    mgr,
		u:      u,
	}

	go grpc.Run()

	<-ctx.Done()
	s.Log.Info("shutting down Node ISP")
}

func (s *Server) storeState() error {
	path := filepath.Join(s.Config.Storage.Data, "state.json")

	f, err := os.Create(path)
	if err != nil {
		s.Log.WithError(err).Error("Failed to create state file")
		return err
	}

	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "\t")

	if err := enc.Encode(s.mgr); err != nil {
		s.Log.WithError(err).Error("Failed to write state file")
		return err
	}

	return nil
}

func (s *Server) loadState() error {
	path := filepath.Join(s.Config.Storage.Data, "state.json")

	f, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	return json.Unmarshal(f, s.mgr)
}

func (s *Server) setupProxy() *httputil.ReverseProxy {
	return &httputil.ReverseProxy{Director: func(req *http.Request) {
		targetQuery := proxyHost.RawQuery
		req.URL.Scheme = proxyHost.Scheme
		req.URL.Host = proxyHost.Host
		req.URL.Path, req.URL.RawPath = joinURLPath(proxyHost, req.URL)
		if targetQuery == "" || req.URL.RawQuery == "" {
			req.URL.RawQuery = targetQuery + req.URL.RawQuery
		} else {
			req.URL.RawQuery = targetQuery + "&" + req.URL.RawQuery
		}
	}}
}

func mkdir(path string) {
	if err := os.MkdirAll(path, 0755); err != nil {
		log.WithError(err).Fatal("Failed to create directory")
	}
}

func absolutePath(path string) string {
	if filepath.IsAbs(path) {
		return path
	}

	path, err := filepath.Abs(path)
	if err != nil {
		log.WithError(err).Fatal("Failed to get absolute path")
	}

	return path
}

func randomFreePort() int {
	randPort := rand.Int() % 10000
	port := 8000 + randPort

	for {
		ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
		if err == nil {
			ln.Close()
			break
		}
		port++
	}

	return port
}

func joinURLPath(a, b *url.URL) (path, rawpath string) {
	if a.RawPath == "" && b.RawPath == "" {
		return singleJoiningSlash(a.Path, b.Path), ""
	}
	// Same as singleJoiningSlash, but uses EscapedPath to determine
	// whether a slash should be added
	apath := a.EscapedPath()
	bpath := b.EscapedPath()

	aslash := strings.HasSuffix(apath, "/")
	bslash := strings.HasPrefix(bpath, "/")

	switch {
	case aslash && bslash:
		return a.Path + b.Path[1:], apath + bpath[1:]
	case !aslash && !bslash:
		return a.Path + "/" + b.Path, apath + "/" + bpath
	}
	return a.Path + b.Path, apath + bpath
}

func singleJoiningSlash(a, b string) string {
	aslash := strings.HasSuffix(a, "/")
	bslash := strings.HasPrefix(b, "/")
	switch {
	case aslash && bslash:
		return a + b[1:]
	case !aslash && !bslash:
		return a + "/" + b
	}
	return a + b
}
