package radius

import (
	"github.com/timshannon/bolthold"
)

type routerManager struct {
	cache *bolthold.Store
}

func (r *routerManager) addRouter(rtr *Router) error {
	ip := rtr.RadiusIp
	if ip == "" {
		ip = rtr.Ip
	}

	return r.cache.Upsert(ip, rtr)
}

func (r *routerManager) getRouter(ip string) (*Router, error) {
	var router Router
	return &router, r.cache.Get(ip, &router)
}
