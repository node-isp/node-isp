package radius

import (
	"github.com/timshannon/bolthold"
)

type serviceManager struct {
	cache *bolthold.Store
}

func (r *serviceManager) addService(service *Service) error {
	return r.cache.Upsert(service.Username, service)
}

func (r *serviceManager) getService(username string) (*Service, error) {
	var service Service
	return &service, r.cache.Get(username, &service)
}
