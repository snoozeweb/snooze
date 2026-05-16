package patlite

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// recorder captures the inbound HTTP request to the stub Patlite device.
type recorder struct {
	mu     sync.Mutex
	hits   atomic.Int64
	last   *http.Request
	lastQ  url.Values
	status int
}

func (r *recorder) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		clone := req.Clone(req.Context())
		r.last = clone
		r.lastQ = req.URL.Query()
		r.mu.Unlock()
		r.hits.Add(1)
		status := r.status
		if status == 0 {
			status = http.StatusOK
		}
		w.WriteHeader(status)
	}
}

func (r *recorder) lastQuery() url.Values {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastQ
}

// hostPortOf extracts host and port from a `http://host:port` URL.
func hostPortOf(t *testing.T, raw string) (host string, port int) {
	t.Helper()
	u, err := url.Parse(raw)
	require.NoError(t, err)
	p, err := strconvAtoi(u.Port())
	require.NoError(t, err)
	return u.Hostname(), p
}

// strconvAtoi is split out so the test file imports stay tidy.
func strconvAtoi(s string) (int, error) {
	var n int
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return 0, &parseError{s: s}
		}
		n = n*10 + int(ch-'0')
	}
	return n, nil
}

type parseError struct{ s string }

func (e *parseError) Error() string { return "parse port: " + e.s }

func newPlugin(t *testing.T) *Plugin {
	t.Helper()
	p, err := factory(plugins.Metadata{Name: "patlite"})
	require.NoError(t, err)
	require.NoError(t, p.PostInit(context.Background(), nil))
	return p.(*Plugin)
}

func TestPatliteSend(t *testing.T) {
	t.Run("severity_maps_to_configured_color", func(t *testing.T) {
		rec := &recorder{}
		srv := httptest.NewServer(rec.handler())
		t.Cleanup(srv.Close)

		host, port := hostPortOf(t, srv.URL)
		p := newPlugin(t)

		payload := plugins.NotificationPayload{
			Meta: map[string]any{
				"host": host,
				"port": port,
				"path": "/", // httptest mux roots on "/"
				"severity_map": map[string]any{
					"critical": map[string]any{"color": "red", "state": "on"},
					"warning":  map[string]any{"color": "amber", "state": "blink1"},
					"default":  map[string]any{"color": "clear"},
				},
			},
		}

		err := p.Send(context.Background(),
			snoozetypes.Record{UID: "u1", Severity: "critical"},
			payload)
		require.NoError(t, err)
		require.EqualValues(t, 1, rec.hits.Load())

		q := rec.lastQuery()
		require.Equal(t, "red", q.Get("color"))
		require.Equal(t, "on", q.Get("state"))
		require.Empty(t, q.Get("clear"))

		// Warning routes to amber/blink1 on the next call.
		require.NoError(t, p.Send(context.Background(),
			snoozetypes.Record{UID: "u2", Severity: "warning"},
			payload))
		q = rec.lastQuery()
		require.Equal(t, "amber", q.Get("color"))
		require.Equal(t, "blink1", q.Get("state"))
	})

	t.Run("unknown_severity_falls_back_to_default_entry", func(t *testing.T) {
		rec := &recorder{}
		srv := httptest.NewServer(rec.handler())
		t.Cleanup(srv.Close)

		host, port := hostPortOf(t, srv.URL)
		p := newPlugin(t)

		err := p.Send(context.Background(),
			snoozetypes.Record{UID: "u-unk", Severity: "lavender"},
			plugins.NotificationPayload{
				Meta: map[string]any{
					"host": host,
					"port": port,
					"path": "/",
					"severity_map": map[string]any{
						"critical": map[string]any{"color": "red", "state": "on"},
						"default":  map[string]any{"color": "clear"},
					},
				},
			})
		require.NoError(t, err)

		q := rec.lastQuery()
		require.Equal(t, "1", q.Get("clear"))
		require.Empty(t, q.Get("color"))
		require.Empty(t, q.Get("state"))
	})

	t.Run("uses_package_default_severity_map_when_omitted", func(t *testing.T) {
		// No severity_map in meta → falls back to defaultSeverityMap().
		rec := &recorder{}
		srv := httptest.NewServer(rec.handler())
		t.Cleanup(srv.Close)

		host, port := hostPortOf(t, srv.URL)
		p := newPlugin(t)

		require.NoError(t, p.Send(context.Background(),
			snoozetypes.Record{UID: "u-warn", Severity: "warning"},
			plugins.NotificationPayload{Meta: map[string]any{
				"host": host,
				"port": port,
				"path": "/",
			}}))
		q := rec.lastQuery()
		require.Equal(t, "amber", q.Get("color"))
		require.Equal(t, "on", q.Get("state"))
	})

	t.Run("error_status_propagates", func(t *testing.T) {
		rec := &recorder{status: http.StatusInternalServerError}
		srv := httptest.NewServer(rec.handler())
		t.Cleanup(srv.Close)

		host, port := hostPortOf(t, srv.URL)
		p := newPlugin(t)

		err := p.Send(context.Background(),
			snoozetypes.Record{UID: "u-err", Severity: "critical"},
			plugins.NotificationPayload{Meta: map[string]any{
				"host": host,
				"port": port,
				"path": "/",
			}})
		require.Error(t, err)
		require.Contains(t, err.Error(), "unexpected status 500")
	})

	t.Run("missing_host_returns_validation_error", func(t *testing.T) {
		p := newPlugin(t)
		err := p.Send(context.Background(),
			snoozetypes.Record{Severity: "critical"},
			plugins.NotificationPayload{}) // empty meta
		require.Error(t, err)
		require.Contains(t, err.Error(), "host is required")
	})

	t.Run("timeout_is_honoured", func(t *testing.T) {
		// Server intentionally hangs longer than the request timeout.
		srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			<-r.Context().Done()
		}))
		t.Cleanup(srv.Close)

		host, port := hostPortOf(t, srv.URL)
		p := newPlugin(t)

		start := time.Now()
		err := p.Send(context.Background(),
			snoozetypes.Record{Severity: "critical"},
			plugins.NotificationPayload{Meta: map[string]any{
				"host":    host,
				"port":    port,
				"path":    "/",
				"timeout": "100ms",
			}})
		require.Error(t, err)
		require.Less(t, time.Since(start), 2*time.Second,
			"request should have timed out well before the 2s safety margin")
	})
}

func TestBuildURL(t *testing.T) {
	t.Run("clear_emits_clear_query", func(t *testing.T) {
		got, err := buildURL(Config{Host: "h", Port: 80, Path: "/api/control"},
			SeverityAction{Color: "clear"})
		require.NoError(t, err)
		require.True(t, strings.HasSuffix(got, "?clear=1"))
	})

	t.Run("color_state_emits_pair", func(t *testing.T) {
		got, err := buildURL(Config{Host: "host", Port: 80, Path: "api/control"},
			SeverityAction{Color: "Red", State: "On"})
		require.NoError(t, err)
		require.Contains(t, got, "color=red")
		require.Contains(t, got, "state=on")
		// Path leading slash is added by buildURL.
		require.Contains(t, got, "/api/control")
	})

	t.Run("empty_state_defaults_to_on", func(t *testing.T) {
		got, err := buildURL(Config{Host: "h", Port: 80, Path: "/x"},
			SeverityAction{Color: "green"})
		require.NoError(t, err)
		require.Contains(t, got, "state=on")
	})
}

func TestPickAction(t *testing.T) {
	m := map[string]SeverityAction{
		"critical": {Color: "red", State: "on"},
		"default":  {Color: "clear"},
	}
	require.Equal(t, SeverityAction{Color: "red", State: "on"},
		pickAction(m, "critical"))
	require.Equal(t, SeverityAction{Color: "red", State: "on"},
		pickAction(m, "CRITICAL")) // case-insensitive
	require.Equal(t, SeverityAction{Color: "clear"},
		pickAction(m, "lavender")) // falls through to default
	require.Equal(t, SeverityAction{Color: "clear"},
		pickAction(map[string]SeverityAction{}, "anything")) // hard fallback
}

func TestConfigDecode(t *testing.T) {
	t.Run("severity_map_string_form_is_color_only", func(t *testing.T) {
		m, err := decodeSeverityMap(map[string]any{"critical": "red"})
		require.NoError(t, err)
		require.Equal(t, "red", m["critical"].Color)
		require.Equal(t, "", m["critical"].State)
	})

	t.Run("rejects_non_object_top_level", func(t *testing.T) {
		_, err := decodeSeverityMap(42)
		require.Error(t, err)
	})
}
