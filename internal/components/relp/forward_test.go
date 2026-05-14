package relp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/japannext/snooze/internal/components/syslog"
	"github.com/japannext/snooze/pkg/snoozeclient"
	"github.com/japannext/snooze/pkg/snoozetypes"
)

// TestToRelpRecordSource confirms the package-local mapper overrides the
// syslog package's Source ("syslog") with "relp" — this is the only thing
// that distinguishes RELP-delivered records from plain syslog ones.
func TestToRelpRecordSource(t *testing.T) {
	t.Parallel()
	msg := syslog.ParsedMessage{
		Format:    "rfc5424",
		Severity:  "info",
		Hostname:  "host.example",
		AppName:   "myapp",
		Message:   "hello",
		Timestamp: time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC),
		HasTime:   true,
		Raw:       "<14>1 2024-01-02T03:04:05Z host.example myapp - - - hello",
	}
	rec := toRelpRecord(msg, "10.0.0.1:54321")
	require.Equal(t, "relp", rec.Source)
	require.Equal(t, "host.example", rec.Host)
	require.Equal(t, "myapp", rec.Process)
	require.Equal(t, "info", rec.Severity)
	require.Equal(t, "hello", rec.Message)
	require.Equal(t, "10.0.0.1:54321", rec.Raw["peer"])
}

// TestForwardEndToEnd wires up a httptest server pretending to be Snooze,
// pushes a syslog payload through Forward, and asserts the POST body matches
// the expected record.
func TestForwardEndToEnd(t *testing.T) {
	t.Parallel()

	var (
		mu       sync.Mutex
		gotPath  string
		gotAuth  string
		gotBody  snoozetypes.Record
		bodyOnce sync.Once
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/v1/login/"):
			// Login handshake — return a fake bearer token.
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"token":"tok","expires_at":"2099-01-01T00:00:00Z","method":"local"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/alerts":
			mu.Lock()
			gotPath = r.URL.Path
			gotAuth = r.Header.Get("Authorization")
			body, _ := io.ReadAll(r.Body)
			bodyOnce.Do(func() {
				_ = json.Unmarshal(body, &gotBody)
			})
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"host":"host.example","source":"relp"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	// Use a unique cache file path so concurrent tests don't share state.
	cache := t.TempDir() + "/token"
	client, err := snoozeclient.New(snoozeclient.Options{
		BaseURL:        srv.URL,
		Username:       "ingest",
		Password:       "secret",
		TokenCacheFile: cache,
	})
	require.NoError(t, err)
	require.NoError(t, client.Login(context.Background()))

	fwd, err := NewForwarder(client, "auto")
	require.NoError(t, err)

	payload := []byte("<14>1 2024-01-02T03:04:05Z host.example myapp - - - hello world")
	require.NoError(t, fwd.Forward(context.Background(), payload, "10.0.0.1:54321"))

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, "/api/v1/alerts", gotPath)
	require.Equal(t, "Bearer tok", gotAuth)
	require.Equal(t, "host.example", gotBody.Host)
	require.Equal(t, "relp", gotBody.Source)
	require.Equal(t, "myapp", gotBody.Process)
	require.Contains(t, gotBody.Message, "hello world")
}

// TestForwardRejectsBadPayload confirms parse errors propagate so the
// listener can NACK without involving the HTTP layer.
func TestForwardRejectsBadPayload(t *testing.T) {
	t.Parallel()
	// HTTP server should never be hit — fail loudly if it is.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Errorf("HTTP server should not be hit on parse error")
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client, err := snoozeclient.New(snoozeclient.Options{
		BaseURL:        srv.URL,
		Token:          "static",
		TokenCacheFile: t.TempDir() + "/token",
	})
	require.NoError(t, err)
	fwd, err := NewForwarder(client, "rfc5424")
	require.NoError(t, err)
	// strict RFC5424 mode plus a clearly-non-syslog payload guarantees an
	// error.
	err = fwd.Forward(context.Background(), []byte(""), "")
	require.Error(t, err)
}
