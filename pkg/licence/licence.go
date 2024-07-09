package licence

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/apex/log"

	"github.com/node-isp/node-isp/pkg/database"
)

type Licence struct {
	db       *database.Database // Add a field to store the database connection
	log      *log.Entry
	fileName string

	ID       string `json:"id"`
	Code     string `json:"-"`
	Domain   string `json:"domain"`
	Valid    bool   `json:"valid"`
	Features struct {
		MultiTenancy bool `json:"multiTenancy"`
		Billing      bool `json:"billing"`
		Helpdesk     bool `json:"helpdesk"`
	} `json:"features"`
	Limits struct {
		Accounts  int `json:"accounts"`
		Customers int `json:"customers"`
		Services  int `json:"services"`
	} `json:"limits"`
	ExpiresAt   interface{} `json:"expires_at"`
	LicenceData string      `json:"licence_data"`
}

func New(id, code string) (*Licence, error) {
	l := &Licence{
		log:  log.WithField("component", "licence"),
		ID:   id,
		Code: code,
	}

	if err := l.validate(); err != nil {
		return nil, err
	}

	// Start a goroutine to refresh the licence every 12 hours
	go func() {
		ticker := time.NewTicker(12 * time.Hour)
		defer ticker.Stop()

		for range ticker.C {
			log.WithField("component", "licence").Info("refreshing licence")

			// refresh the licence
			if err := l.validate(); err != nil {
				l.log.WithError(err).Error("failed to refresh licence")
			}

			l.log.Info("licence refreshed")

		}
	}()

	return l, nil
}

func (l *Licence) validate() error {
	if l.ID == "" {
		return fmt.Errorf("licence ID is required")
	}

	if l.Code == "" {
		return fmt.Errorf("licence code is required")
	}

	url := fmt.Sprintf("https://beta.theitdept.au/api/v1/licence/%s/%s", l.ID, l.Code)

	req, err := http.NewRequestWithContext(context.TODO(), "GET", url, nil)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to validate licence server %d", resp.StatusCode)
	}

	if err := json.NewDecoder(resp.Body).Decode(&l); err != nil {
		return err
	}

	return nil
}

func (l *Licence) Store(fileName string) error {
	// Store the licence data in a file
	decoded, err := base64.StdEncoding.DecodeString(l.LicenceData)
	if err != nil {
		return err
	}

	if err := os.WriteFile(fileName, decoded, 0644); err != nil {
		return err
	}

	l.log.Info("licence data stored in file")

	return nil
}
