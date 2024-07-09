// Package realtime implements a simple Real Time server, which will be used for managing external services.
package realtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/apex/log"
	"github.com/centrifugal/centrifuge"
	"github.com/jmoiron/sqlx"

	"github.com/node-isp/node-isp/pkg/database"
)

type RealTime struct {
	sync.RWMutex
	presence map[string]map[string]*centrifuge.ClientInfo

	Log       *log.Entry
	WSHandler *centrifuge.WebsocketHandler

	BackendUrl string
	DB         *database.Database

	node *centrifuge.Node
}

func (r *RealTime) Run() error {
	node, err := centrifuge.New(centrifuge.Config{
		LogLevel:       centrifuge.LogLevelInfo,
		LogHandler:     r.handleLog,
		HistoryMetaTTL: 24 * time.Hour,
	})

	if err != nil {
		return err
	}

	broker, _ := centrifuge.NewMemoryBroker(node, centrifuge.MemoryBrokerConfig{})
	node.SetBroker(broker)

	if err := node.Run(); err != nil {
		return err
	}

	r.presence = make(map[string]map[string]*centrifuge.ClientInfo)

	node.SetPresenceManager(r)

	node.OnConnecting(r.HandleConnecting)
	node.OnConnect(r.HandleConnect)

	r.node = node

	r.WSHandler = centrifuge.NewWebsocketHandler(node, centrifuge.WebsocketConfig{
		ReadBufferSize:     1024,
		UseWriteBufferPool: true,
	})

	return nil
}

type connectResponse struct {
	Result struct {
		User     string   `json:"user"`
		Channels []string `json:"channels"`
	} `json:"result"`
}

func (r *RealTime) Handler(w http.ResponseWriter, req *http.Request) {
	// Extract token from request header
	token := req.Header.Get("Authorization")
	if token == "" {
		http.Error(w, "missing token", http.StatusUnauthorized)
		return
	}

	// Call the backend api to get authentication details and validate the token
	backendReq, _ := http.NewRequest("POST", fmt.Sprintf("%s/connect", r.BackendUrl), nil)
	backendReq.Header.Set("Authorization", req.Header.Get("Authorization"))
	backendReq.Header.Set("accept", "application/json")

	// If we have X-Tenant header, pass it to the backend
	if tenant := req.Header.Get("X-Tenant"); tenant != "" {
		backendReq.Header.Set("X-Tenant", tenant)
	}

	resp, err := http.DefaultClient.Do(backendReq)
	if err != nil {
		r.Log.WithError(err).Error("failed to authenticate with backend")
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}

	if resp.StatusCode != http.StatusOK {
		r.Log.WithError(err).Error("backend error")
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}

	var connectResp connectResponse
	if err := json.NewDecoder(resp.Body).Decode(&connectResp); err != nil {
		r.Log.WithError(err).Error("failed to decode response")
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}

	ctx := centrifuge.SetCredentials(req.Context(), &centrifuge.Credentials{
		UserID:   connectResp.Result.User,
		ExpireAt: time.Now().Add(24 * time.Hour).Unix(),
	})

	ctx = context.WithValue(ctx, "user_channels", &connectResp.Result.Channels)
	ctx = context.WithValue(ctx, "backend_token", req.Header.Get("Authorization"))
	ctx = context.WithValue(ctx, "backend_tenant", req.Header.Get("X-Tenant"))

	r.WSHandler.ServeHTTP(w, req.WithContext(ctx))
}

func (r *RealTime) HandleConnecting(ctx context.Context, e centrifuge.ConnectEvent) (centrifuge.ConnectReply, error) {
	cred, _ := centrifuge.GetCredentials(ctx)
	r.Log.
		WithField("user", cred.UserID).
		WithField("transport", e.Transport.Name()).
		WithField("protocol", e.Transport.Protocol()).
		Info("connecting")

	subs := make(map[string]centrifuge.SubscribeOptions)

	for _, c := range *ctx.Value("user_channels").(*[]string) {
		subs[c] = centrifuge.SubscribeOptions{
			EmitPresence: true,
		}
	}

	return centrifuge.ConnectReply{
		Subscriptions: subs,
	}, nil
}

type rpcRequest struct {
	Method string          `json:"method"`
	Data   json.RawMessage `json:"data"`
}

type rpcReply struct {
	Result struct {
		Data json.RawMessage `json:"data"`
	} `json:"result"`
}

func (r *RealTime) HandleConnect(client *centrifuge.Client) {
	transport := client.Transport()
	r.Log.
		WithField("user", client.UserID()).
		WithField("transport", transport.Name()).
		WithField("protocol", transport.Protocol()).
		Info("connected")

	client.OnRPC(func(e centrifuge.RPCEvent, cb centrifuge.RPCCallback) {
		go func() {
			defer func() {
				if rec := recover(); rec != nil {
					r.Log.WithError(rec.(error)).Error("panic in rpc handler")
					cb(centrifuge.RPCReply{}, fmt.Errorf("panic in rpc handler"))
				}
			}()

			r.Log.
				WithField("user", client.UserID()).
				WithField("method", e.Method).
				WithField("data", string(e.Data)).
				Info("rpc call")

			// Post to the backend RPC handler, and return the response
			body := new(bytes.Buffer)
			err := json.NewEncoder(body).Encode(rpcRequest{
				Method: e.Method,
				Data:   e.Data,
			})
			if err != nil {
				cb(centrifuge.RPCReply{}, err)
			}

			backendReq, _ := http.NewRequest("POST", fmt.Sprintf("%s/rpc", r.BackendUrl), body)
			backendReq.Header.Set("Authorization", client.Context().Value("backend_token").(string))
			backendReq.Header.Set("accept", "application/json")
			backendReq.Header.Set("content-type", "application/json")
			tenant := client.Context().Value("backend_tenant")
			if tenant != nil {
				backendReq.Header.Set("X-Tenant", tenant.(string))
			}

			resp, err := http.DefaultClient.Do(backendReq)
			if err != nil {
				panic(err)
			}

			var rep rpcReply
			if err := json.NewDecoder(resp.Body).Decode(&rep); err != nil {
				log.WithError(err).Error("failed to decode response")
			}

			cb(centrifuge.RPCReply{Data: rep.Result.Data}, nil)

		}()
	})

	return
}

func (r *RealTime) updateRadiusServerStatus(radiusServerId, status string) error {

	// Ignore if db not set
	// This isn't used in the standalone realtime server, so not needed..
	if r.DB == nil {
		return nil
	}

	r.Log.
		WithField("radius_server_id", radiusServerId).
		WithField("status", status).
		Info("updating radius server status")

	q := "UPDATE radius_servers SET status = $1, last_seen_at=now() WHERE id = $2"
	qry, args, err := sqlx.In(q, status, radiusServerId)
	if err != nil {
		return err
	}

	_, err = r.DB.Exec(qry, args...)
	return err
}

func (r *RealTime) handleLog(e centrifuge.LogEntry) {
	r.Log.WithFields(logProxy{e}).Info(e.Message)
}

type logProxy struct {
	centrifuge.LogEntry
}

func (l logProxy) Fields() log.Fields {
	return l.LogEntry.Fields
}
