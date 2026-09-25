package fetch

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"gabe565.com/nightscout-menu-bar/internal/config"
	"gabe565.com/nightscout-menu-bar/internal/librelinkup"
	"gabe565.com/nightscout-menu-bar/internal/nightscout"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testUsername  = "user@example.com"
	testPassword  = "hunter2"
	testUserID    = "user-id"
	testPatientID = "patient-id"
	testToken     = "token"
)

type fakeLLU struct {
	*httptest.Server
	logins     atomic.Int32
	graphs     atomic.Int32
	expireNext atomic.Bool
}

func newFakeLLU(t *testing.T) *fakeLLU {
	t.Helper()
	f := &fakeLLU{}
	sum := sha256.Sum256([]byte(testUserID))
	accountID := hex.EncodeToString(sum[:])

	mux := http.NewServeMux()
	mux.HandleFunc("POST /{region}/llu/auth/login", func(w http.ResponseWriter, r *http.Request) {
		f.logins.Add(1)
		var body map[string]string
		if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&body)) {
			return
		}
		if body["email"] != testUsername || body["password"] != testPassword {
			_, _ = w.Write([]byte(`{"status":2,"error":{"message":"Bad credentials"}}`))
			return
		}
		if r.PathValue("region") == "global" {
			_, _ = w.Write([]byte(`{"status":0,"data":{"redirect":true,"region":"us"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"status":0,"data":{"user":{"id":"` + testUserID + `"},"authTicket":{"token":"` + testToken + `","expires":` +
			jsonInt(time.Now().Add(time.Hour).Unix()) + `,"duration":3600000}}}`))
	})
	authed := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.PathValue("region") != "us" ||
				r.Header.Get("Authorization") != "Bearer "+testToken ||
				r.Header.Get("Account-Id") != accountID ||
				r.Header.Get("Product") != librelinkup.Product ||
				r.Header.Get("Version") == "" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			if f.expireNext.CompareAndSwap(true, false) {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			next(w, r)
		}
	}
	mux.HandleFunc("GET /{region}/llu/connections", authed(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":0,"data":[{"patientId":"` + testPatientID + `","firstName":"Test","lastName":"User"}]}`))
	}))
	mux.HandleFunc("GET /{region}/llu/connections/{id}/graph", authed(func(w http.ResponseWriter, r *http.Request) {
		f.graphs.Add(1)
		if !assert.Equal(t, testPatientID, r.PathValue("id")) {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(testGraph))
	}))

	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Close)
	return f
}

func (f *fakeLLU) newFetch(username, password string) *Fetch {
	fetch := NewFetch(config.New(config.WithData(config.Data{
		LibreLinkUp: config.LibreLinkUp{Username: username, Password: password},
		Advanced:    config.Advanced{APIVersion: "4.16.0"},
	})))
	fetch.client.RegionURL = func(region string) string {
		if region == "" {
			region = "global"
		}
		return f.URL + "/" + region
	}
	return fetch
}

func jsonInt(i int64) string {
	b, _ := json.Marshal(i)
	return string(b)
}

const testGraph = `{
  "status": 0,
  "data": {
    "connection": {
      "patientId": "patient-id",
      "glucoseMeasurement": {"FactoryTimestamp": "10/2/2022 9:31:00 PM", "ValueInMgPerDl": 123, "TrendArrow": 4}
    },
    "graphData": [
      {"FactoryTimestamp": "10/2/2022 9:11:00 PM", "ValueInMgPerDl": 110},
      {"FactoryTimestamp": "10/2/2022 9:16:00 PM", "ValueInMgPerDl": 113},
      {"FactoryTimestamp": "10/2/2022 9:21:00 PM", "ValueInMgPerDl": 116},
      {"FactoryTimestamp": "10/2/2022 9:26:00 PM", "ValueInMgPerDl": 119},
      {"FactoryTimestamp": "10/2/2022 9:30:30 PM", "ValueInMgPerDl": 122}
    ]
  }
}`

func TestNewFetch(t *testing.T) {
	t.Parallel()
	fetch := NewFetch(config.New())
	require.NotNil(t, fetch)
	assert.NotNil(t, fetch.config)
	assert.NotNil(t, fetch.client)
}

func TestFetch_Do(t *testing.T) {
	t.Parallel()

	t.Run("no credentials", func(t *testing.T) {
		t.Parallel()
		server := newFakeLLU(t)
		_, err := server.newFetch("", "").Do(t.Context())
		require.ErrorIs(t, err, ErrNoCredentials)
		assert.Zero(t, server.logins.Load())
	})

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		server := newFakeLLU(t)
		fetch := server.newFetch(testUsername, testPassword)

		got, err := fetch.Do(t.Context())
		require.NoError(t, err)
		assert.Equal(t, nightscout.Mgdl(123), got.Bgnow.Last)
		assert.Equal(t, "FortyFiveUp", got.Direction.Value)
		// Global login plus the redirected regional login
		assert.EqualValues(t, 2, server.logins.Load())

		// The session is reused
		_, err = fetch.Do(t.Context())
		require.NoError(t, err)
		assert.EqualValues(t, 2, server.logins.Load())
		assert.EqualValues(t, 2, server.graphs.Load())
	})

	t.Run("expired session logs in again", func(t *testing.T) {
		t.Parallel()
		server := newFakeLLU(t)
		fetch := server.newFetch(testUsername, testPassword)
		_, err := fetch.Do(t.Context())
		require.NoError(t, err)

		server.expireNext.Store(true)
		_, err = fetch.Do(t.Context())
		require.NoError(t, err)
		assert.EqualValues(t, 4, server.logins.Load())
	})

	t.Run("rejected credentials are not retried", func(t *testing.T) {
		t.Parallel()
		server := newFakeLLU(t)
		fetch := server.newFetch(testUsername, "wrong")

		_, err := fetch.Do(t.Context())
		require.ErrorIs(t, err, librelinkup.ErrInvalidCredentials)
		_, err = fetch.Do(t.Context())
		require.ErrorIs(t, err, librelinkup.ErrInvalidCredentials)
		assert.EqualValues(t, 1, server.logins.Load())

		// Updating the password retries
		data := fetch.config.Data()
		data.LibreLinkUp.Password = testPassword
		fetch.config = config.New(config.WithData(data))
		_, err = fetch.Do(t.Context())
		require.NoError(t, err)
	})
}

func TestNewProperties(t *testing.T) {
	t.Parallel()
	var resp librelinkup.Response[librelinkup.GraphData]
	require.NoError(t, json.Unmarshal([]byte(testGraph), &resp))

	got := NewProperties(&resp.Data)

	recent := time.Date(2022, time.October, 2, 21, 31, 0, 0, time.UTC)
	assert.True(t, recent.Equal(got.Bgnow.Mills.Time))
	assert.Equal(t, nightscout.Mgdl(123), got.Bgnow.Last)
	require.Len(t, got.Bgnow.Sgvs, 1)
	assert.Equal(t, "FortyFiveUp", got.Bgnow.Sgvs[0].Direction)

	// Current reading first, then history newest first, skipping the reading within a minute of the current one
	require.Len(t, got.Buckets, 5)
	lasts := make([]nightscout.Mgdl, 0, len(got.Buckets))
	for _, b := range got.Buckets {
		lasts = append(lasts, b.Last)
	}
	assert.Equal(t, []nightscout.Mgdl{123, 119, 116, 113, 110}, lasts)
	assert.Empty(t, got.Buckets[1].Sgvs)

	// Delta against 9:26 (5 minutes earlier)
	require.True(t, got.Delta.Valid())
	assert.Equal(t, nightscout.Mgdl(4), got.Delta.Mgdl)
	assert.False(t, got.Delta.Interpolated)
}

func TestNewProperties_scaledDelta(t *testing.T) {
	t.Parallel()
	now := time.Now()
	graph := &librelinkup.GraphData{
		Connection: librelinkup.Connection{
			GlucoseMeasurement: librelinkup.GlucoseItem{
				FactoryTimestamp: librelinkup.Timestamp{Time: now},
				ValueInMgPerDl:   130,
			},
		},
		GraphData: []librelinkup.GlucoseItem{
			{FactoryTimestamp: librelinkup.Timestamp{Time: now.Add(-15 * time.Minute)}, ValueInMgPerDl: 100},
		},
	}
	got := NewProperties(graph)
	require.True(t, got.Delta.Valid())
	assert.Equal(t, nightscout.Mgdl(10), got.Delta.Mgdl)
	assert.True(t, got.Delta.Interpolated)

	graph.GraphData[0].FactoryTimestamp.Time = now.Add(-time.Hour)
	got = NewProperties(graph)
	assert.False(t, got.Delta.Valid())
}

func TestChoosePatient(t *testing.T) {
	t.Parallel()
	connections := []librelinkup.Connection{{PatientID: "a"}, {PatientID: "b"}}

	_, err := choosePatient(nil, "")
	require.ErrorIs(t, err, ErrNoConnections)

	got, err := choosePatient(connections, "")
	require.NoError(t, err)
	assert.Equal(t, "a", got)

	got, err = choosePatient(connections, "b")
	require.NoError(t, err)
	assert.Equal(t, "b", got)

	_, err = choosePatient(connections, "c")
	require.ErrorIs(t, err, ErrPatientNotFound)
}
