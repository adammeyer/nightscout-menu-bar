package fetch

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"gabe565.com/nightscout-menu-bar/internal/config"
	"gabe565.com/nightscout-menu-bar/internal/librelinkup"
	"gabe565.com/nightscout-menu-bar/internal/nightscout"
)

var (
	ErrNoCredentials   = errors.New("please configure your LibreLinkUp username and password")
	ErrNoConnections   = errors.New("no LibreLinkUp connections found; make sure you are following someone in the LibreLinkUp app")
	ErrPatientNotFound = errors.New("configured LibreLinkUp patient-id was not found")
	ErrNoReading       = errors.New("LibreLinkUp has no current reading")
)

func NewFetch(conf *config.Config) *Fetch {
	return &Fetch{
		config: conf,
		client: librelinkup.New(&http.Client{
			Timeout: time.Minute,
		}),
	}
}

type Fetch struct {
	mu        sync.Mutex
	config    *config.Config
	client    *librelinkup.Client
	session   [sha256.Size]byte
	patientID string
	// rejected is set when LibreLinkUp rejects the configured credentials.
	// Retrying the same credentials can lock the account, so they will not be retried until they change.
	rejected bool
}

func (f *Fetch) Do(ctx context.Context) (*nightscout.Properties, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	start := time.Now()
	data := f.config.Data()
	creds := data.LibreLinkUp
	if creds.Username == "" || creds.Password == "" {
		return nil, ErrNoCredentials
	}

	if session := sessionKey(creds); session != f.session {
		slog.Debug("LibreLinkUp settings changed; starting new session")
		f.client.Logout()
		f.session = session
		f.patientID = ""
		f.rejected = false
	}
	if f.rejected {
		return nil, fmt.Errorf("%w; update it in Preferences", librelinkup.ErrInvalidCredentials)
	}

	f.client.Version = data.Advanced.APIVersion

	graph, err := f.fetchGraph(ctx, creds)
	if errors.Is(err, librelinkup.ErrUnauthorized) {
		// The session was revoked or expired early, so log in again once
		f.client.Logout()
		f.patientID = ""
		graph, err = f.fetchGraph(ctx, creds)
	}
	if err != nil {
		if errors.Is(err, librelinkup.ErrInvalidCredentials) {
			f.rejected = true
		}
		return nil, err
	}

	if graph.Connection.GlucoseMeasurement.FactoryTimestamp.IsZero() {
		return nil, ErrNoReading
	}

	properties := NewProperties(graph)
	slog.Debug("Parsed response", "took", time.Since(start), "data", properties)
	return properties, nil
}

func (f *Fetch) fetchGraph(ctx context.Context, creds config.LibreLinkUp) (*librelinkup.GraphData, error) {
	if !f.client.LoggedIn() {
		if err := f.client.Login(ctx, creds.Username, creds.Password, creds.Region); err != nil {
			return nil, err
		}
	}

	if f.patientID == "" {
		connections, err := f.client.Connections(ctx)
		if err != nil {
			return nil, err
		}

		patientID, err := choosePatient(connections, creds.PatientID)
		if err != nil {
			return nil, err
		}
		f.patientID = patientID
	}

	return f.client.Graph(ctx, f.patientID)
}

func choosePatient(connections []librelinkup.Connection, patientID string) (string, error) {
	if len(connections) == 0 {
		return "", ErrNoConnections
	}

	if patientID == "" {
		if len(connections) > 1 {
			for _, c := range connections {
				slog.Info("Found LibreLinkUp connection", "name", c.FirstName+" "+c.LastName, "patient-id", c.PatientID)
			}
			slog.Warn("Multiple LibreLinkUp connections found; using the first one. Set librelinkup.patient-id to choose another.")
		}
		return connections[0].PatientID, nil
	}

	for _, c := range connections {
		if c.PatientID == patientID {
			return c.PatientID, nil
		}
	}
	return "", ErrPatientNotFound
}

func sessionKey(creds config.LibreLinkUp) [sha256.Size]byte {
	return sha256.Sum256([]byte(creds.Username + "\x00" + creds.Password + "\x00" + creds.Region + "\x00" + creds.PatientID))
}
