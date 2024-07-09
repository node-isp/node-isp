package realtime

import (
	"github.com/centrifugal/centrifuge"
)

var _ centrifuge.PresenceManager = (*RealTime)(nil)

func (r *RealTime) Presence(ch string) (map[string]*centrifuge.ClientInfo, error) {
	r.RLock()
	defer r.RUnlock()

	if _, ok := r.presence[ch]; !ok {
		return nil, nil
	}

	return r.presence[ch], nil
}

func (r *RealTime) PresenceStats(ch string) (centrifuge.PresenceStats, error) {
	r.RLock()
	defer r.RUnlock()

	if _, ok := r.presence[ch]; !ok {
		return centrifuge.PresenceStats{}, nil
	}

	numClients := len(r.presence[ch])
	numUsers := 0
	uniqueUsers := make(map[string]struct{})

	for _, info := range r.presence[ch] {
		userID := info.UserID
		if _, ok := uniqueUsers[userID]; !ok {
			uniqueUsers[userID] = struct{}{}
			numUsers++
		}
	}

	return centrifuge.PresenceStats{
		NumClients: numClients,
		NumUsers:   numUsers,
	}, nil

}

func (r *RealTime) AddPresence(ch string, clientID string, info *centrifuge.ClientInfo) error {
	r.Lock()

	if _, ok := r.presence[ch]; !ok {
		r.presence[ch] = make(map[string]*centrifuge.ClientInfo)
	}

	r.presence[ch][clientID] = info
	r.Unlock()

	// Start a goroutine to update presence in the database
	go func() {
		err := r.updateRadiusServerStatus(info.UserID, "ONLINE")
		if err != nil {
			r.Log.WithError(err).Error("Error updating radius server status")
		}
	}()
	return nil
}

func (r *RealTime) RemovePresence(ch string, clientID string, userID string) error {
	r.Lock()
	defer r.Unlock()

	if _, ok := r.presence[ch]; !ok {
		return nil
	}

	delete(r.presence[ch], clientID)

	// Start a goroutine to update presence in the database
	go func() {
		err := r.updateRadiusServerStatus(userID, "OFFLINE")
		if err != nil {
			r.Log.WithError(err).Error("Error updating radius server status")
		}
	}()

	return nil
}
