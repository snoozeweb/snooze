package snoozeclient_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/pkg/snoozeclient"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

func TestClient_IngestToken_SentAsBearer(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		// Return a valid alerts response.
		_, _ = w.Write([]byte(`{"data":[{"uid":"u1"}]}`))
	}))
	defer srv.Close()

	c, err := snoozeclient.New(snoozeclient.Options{
		BaseURL:     srv.URL,
		IngestToken: "my-ingest-tok",
	})
	require.NoError(t, err)

	_, err = c.PostAlert(t.Context(), snoozetypes.Record{Host: "h1"})
	require.NoError(t, err)
	require.Equal(t, "Bearer my-ingest-tok", gotAuth)
}

func TestClient_IngestToken_OverridesLoginToken(t *testing.T) {
	// When both IngestToken and a bearer token (from Login) are set,
	// IngestToken wins on every outbound request.
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"uid":"u1"}]}`))
	}))
	defer srv.Close()

	c, err := snoozeclient.New(snoozeclient.Options{
		BaseURL:     srv.URL,
		Token:       "old-bearer",
		IngestToken: "ingest-bearer",
	})
	require.NoError(t, err)

	_, err = c.PostAlert(t.Context(), snoozetypes.Record{Host: "h1"})
	require.NoError(t, err)
	require.Equal(t, "Bearer ingest-bearer", gotAuth)
}
