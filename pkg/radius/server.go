package radius

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/apex/log"
	"github.com/justinas/alice"
	"github.com/throttled/throttled/v2"
	"github.com/throttled/throttled/v2/store/memstore"
)

func (f *Radius) routes() {
	store, err := memstore.NewCtx(65536)

	quota := throttled.RateQuota{
		MaxRate:  throttled.PerMin(20),
		MaxBurst: 5,
	}
	rateLimiter, err := throttled.NewGCRARateLimiterCtx(store, quota)
	if err != nil {
		log.WithError(err).Error("failed to create rate limiter")
	}

	rateLimited := throttled.HTTPRateLimiterCtx{
		RateLimiter: rateLimiter,
		VaryBy:      &throttled.VaryBy{Path: true},
	}

	f.middleware = alice.New(
		rateLimited.RateLimit,
		timeoutHandler,
		logger,
	)

	f.mux.Handle("POST /api/v1/radius/authorize", f.middleware.ThenFunc(f.handleAuthenticate))
	f.mux.Handle("POST /api/v1/radius/authenticate", f.middleware.ThenFunc(f.handleAuthenticate))
	f.mux.Handle("GET /api/v1/radius/client/{ip}", f.middleware.ThenFunc(f.handleClient))
}

type nas struct {
	ID     string `json:"id"`
	IP     string `json:"ip"`
	Name   string `json:"name"`
	Secret string `json:"secret"`
}

type accessRequest struct {
	Request struct {
		UserName         string      `json:"User-Name"`
		CallingStationId string      `json:"Calling-Station-Id"`
		VendorSpecific   interface{} `json:"Vendor-Specific"`
		ServiceType      string      `json:"Service-Type"`
		UserPassword     string      `json:"User-Password"`
		NASPort          int         `json:"NAS-Port"`
		NASIdentifier    string      `json:"NAS-Identifier"`
		NASPortType      string      `json:"NAS-Port-Type"`
		NASPortId        string      `json:"NAS-Port-Id"`
		Net              struct {
			Src struct {
				IP   string `json:"IP"`
				Port int    `json:"Port"`
			} `json:"Src"`
			Dst struct {
				IP   string `json:"IP"`
				Port int    `json:"Port"`
			} `json:"Dst"`
			Timestamp string `json:"Timestamp"`
		} `json:"Net"`
		PacketType string `json:"Packet-Type"`
	} `json:"request"`
}

func (f *Radius) handleAuthenticate(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	b, err := io.ReadAll(r.Body)
	if err != nil {
		log.WithError(err).Error("failed to read body")
		w.WriteHeader(401)
		return
	}

	var a accessRequest
	if err := json.Unmarshal(b, &a); err != nil {
		log.WithError(err).Error("failed to unmarshal body")
		w.WriteHeader(401)
		return
	}

	// Authenticate the user.
	service, err := f.serviceManager.getService(a.Request.UserName)
	if err != nil {
		log.WithError(err).Error("failed to authenticate user with backend")
		w.WriteHeader(401)
		return
	}

	// Encode reply attrs to json
	if err := json.NewEncoder(w).Encode(service.getReplyAttributes()); err != nil {
		log.WithError(err).Error("failed to write response")
		w.WriteHeader(401)
		return
	}
}

func (f *Radius) handleClient(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	ip := r.PathValue("ip")

	// Find the nas with the given IP address.
	n, err := f.routerManager.getRouter(ip)

	if err != nil {
		w.WriteHeader(404)
		return
	}

	// Return the nas.
	if err := json.NewEncoder(w).Encode(&nas{
		ID:     n.Id,
		IP:     ip,
		Name:   n.Name,
		Secret: n.RadiusSecret,
	}); err != nil {
		w.WriteHeader(404)
		return
	}
}

func logger(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.
			WithField("method", r.Method).
			WithField("path", r.URL.Path).
			WithField("ip", r.RemoteAddr).
			Info("request")
		h.ServeHTTP(w, r)
	})
}

func timeoutHandler(h http.Handler) http.Handler {
	return http.TimeoutHandler(h, 1*time.Second, "timed out")
}
