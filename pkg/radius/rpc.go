package radius

import (
	"context"
	"encoding/json"
	"time"

	"github.com/apex/log"
)

type RPCAction string

const (
	GetRouters          RPCAction = "App\\Actions\\Radius\\GetRouters"
	GetServices         RPCAction = "App\\Actions\\Radius\\GetServices"
	AuthenticateService RPCAction = "App\\Actions\\Radius\\AuthenticateService"
)

type Router struct {
	Id                  string      `json:"id"`
	PointOfPresenceId   string      `json:"point_of_presence_id"`
	VendorId            string      `json:"vendor_id"`
	Name                string      `json:"name"`
	Ip                  string      `json:"ip"`
	Model               string      `json:"model"`
	Serial              string      `json:"serial"`
	RadiusIp            string      `json:"radius_ip"`
	RadiusSecret        string      `json:"radius_secret"`
	AuthorizationType   string      `json:"authorization_type"`
	AccountingType      string      `json:"accounting_type"`
	CreatedAt           time.Time   `json:"created_at"`
	UpdatedAt           time.Time   `json:"updated_at"`
	DeletedAt           *time.Time  `json:"deleted_at"`
	ExternalIdentifiers interface{} `json:"external_identifiers"`
}

type ReplyValue struct {
	Op    string   `json:"op"`
	Value []string `json:"value"`
}

type RadReply struct {
	Attribute string `json:"attribute"`
	Op        string `json:"op"`
	Value     string `json:"value"`
}

type Service struct {
	Id string `json:"id"`

	RadiusTemplateId string `json:"radius_template_id"`
	ServiceId        string `json:"service_id"`

	Username string `json:"username"`
	Password string `json:"password"`

	Radreply []RadReply `json:"radreply"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type radiusAuthResponse map[string]*ReplyValue

func (s *Service) getReplyAttributes() *radiusAuthResponse {
	replyAttributes := &radiusAuthResponse{}

	for _, reply := range s.Radreply {
		if _, ok := (*replyAttributes)[reply.Attribute]; !ok {
			(*replyAttributes)[reply.Attribute] = &ReplyValue{
				Op:    reply.Op,
				Value: []string{reply.Value},
			}
		} else {
			(*replyAttributes)[reply.Attribute] = &ReplyValue{
				Op:    reply.Op,
				Value: append((*replyAttributes)[reply.Attribute].Value, reply.Value),
			}
		}

	}

	return replyAttributes
}

type rpcResponse struct {
	Action string          `json:"action"`
	Status string          `json:"status"`
	Result json.RawMessage `json:"result"`
}

func (f *Radius) callRpc(action RPCAction, data []byte) (*rpcResponse, error) {
	res, err := f.client.RPC(context.Background(), string(action), data)
	if err != nil {
		return nil, err
	}

	var response rpcResponse
	err = json.Unmarshal(res.Data, &response)
	if err != nil {
		return nil, err
	}

	return &response, nil
}

type getRouterResponse struct {
	Status  string   `json:"status"`
	Routers []Router `json:"routers"`
}

func (f *Radius) loadRoutersFromAPI() {
	// Load the routers from the API
	res, err := f.callRpc(GetRouters, []byte(`{}`))

	if err != nil {
		log.WithError(err).Error("failed to loadRoutersFromAPI")
		return
	}

	var routers getRouterResponse
	err = json.Unmarshal(res.Result, &routers)
	if err != nil {
		log.WithError(err).Error("failed to unmarshal response")
		return
	}

	// Update the routers
	for _, r := range routers.Routers {
		_ = f.routerManager.addRouter(&r)
	}
}

type getServicesResponse struct {
	Status   string    `json:"status"`
	Services []Service `json:"services"`
}

func (f *Radius) loadServicesFromAPI() {
	res, err := f.callRpc(GetServices, []byte(`{}`))

	if err != nil {
		log.WithError(err).Error("failed to loadServicesFromAPI")
		return
	}

	var serviceResponse getServicesResponse
	err = json.Unmarshal(res.Result, &serviceResponse)

	if err != nil {
		log.WithError(err).Error("failed to unmarshal response")
		return
	}

	for _, s := range serviceResponse.Services {
		_ = f.serviceManager.addService(&s)
	}
}

type authenticateRequest struct {
	Request json.RawMessage `json:"request"`
	Router  string          `json:"router_id"`
}
