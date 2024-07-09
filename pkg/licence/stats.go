package licence

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	_ "github.com/lib/pq"

	"github.com/node-isp/node-isp/pkg/database"
)

type stats struct {
	Companies int `json:"companies"`
	Customers int `json:"customers"`
	Contacts  int `json:"contacts"`
	Services  int `json:"services"`

	ServicesByStatus map[string]int `json:"services_by_status"`
}

func (l *Licence) StartStatsReporter(db *database.Database) error {

	l.db = db
	// Start a goroutine to process stats every 12 hours
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()

		for range ticker.C {
			// process stats
			l.log.Info("sending usage statistics to the licence server")

			// process stats
			s, err := l.stats()
			if err != nil {
				l.log.WithError(err).Error("failed to process stats")
				continue
			}

			// send stats to the licence server
			if err := l.sendStats(s); err != nil {
				l.log.WithError(err).Error("failed to send stats")
				continue
			}

			// Close the database connection
			if err := l.db.Close(); err != nil {
				l.log.WithError(err).Error("failed to close the database connection")
			}

			l.log.WithField("stats", fmt.Sprintf("%+v", s)).Info("usage statistics sent to the licence server")
		}
	}()

	l.log.Info("usage statistics reporter started")

	return nil
}

func (l *Licence) stats() (*stats, error) {
	companies, err := l.countCompanies()
	if err != nil {
		return nil, err
	}

	customers, err := l.countCustomers()
	if err != nil {
		return nil, err
	}

	contacts, err := l.countContacts()
	if err != nil {
		return nil, err
	}

	services, err := l.countServices()
	if err != nil {
		return nil, err
	}

	servicesByStatus, err := l.countServicesByStatus()
	if err != nil {
		return nil, err
	}

	return &stats{
		Companies:        companies,
		Customers:        customers,
		Contacts:         contacts,
		Services:         services,
		ServicesByStatus: servicesByStatus,
	}, nil
}

func (l *Licence) countCompanies() (int, error) {
	count := 0
	return count, l.db.Get(&count, "SELECT COUNT(*) FROM companies")
}

func (l *Licence) countCustomers() (int, error) {
	count := 0
	return count, l.db.Get(&count, "SELECT COUNT(*) FROM customers")
}

func (l *Licence) countContacts() (int, error) {
	count := 0
	return count, l.db.Get(&count, "SELECT COUNT(*) FROM contacts")
}

func (l *Licence) countServices() (int, error) {
	count := 0
	return count, l.db.Get(&count, "SELECT COUNT(*) FROM services")
}

func (l *Licence) countServicesByStatus() (map[string]int, error) {
	counts := make(map[string]int)

	rows, err := l.db.Queryx("SELECT status, COUNT(*) FROM services GROUP BY status")
	if err != nil {
		return counts, nil
	}

	for rows.Next() {
		var status string
		var count int

		if err := rows.Scan(&status, &count); err != nil {
			return counts, err
		}

		counts[strings.ToLower(status)] = count
	}

	return counts, nil
}

func (l *Licence) sendStats(s *stats) error {
	// Send the stats to the licence server
	url := fmt.Sprintf("https://beta.theitdept.au/api/v1/licence/%s/%s/metrics", l.ID, l.Code)

	statsJSON, err := json.Marshal(s)
	if err != nil {
		return err
	}

	req, _ := http.NewRequest("POST", url, bytes.NewBuffer(statsJSON))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to send stats to the licence server %d", resp.StatusCode)
	}

	return nil
}
