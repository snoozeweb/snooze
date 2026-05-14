package webhook

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/japannext/snooze/internal/plugins"
	"github.com/japannext/snooze/pkg/snoozetypes"
)

func TestRegistration(t *testing.T) {
	require.True(t, slices.Contains(plugins.Registered(), "webhook"))
}

func newPluginForTest(t *testing.T) *Plugin {
	t.Helper()
	p, err := factory(plugins.Metadata{Name: "webhook"})
	require.NoError(t, err)
	wp, ok := p.(*Plugin)
	require.True(t, ok)
	// Replace the client builder with one that reuses the httptest server's
	// default transport. The default builder builds a TLS-aware client; for
	// plain httptest.NewServer the default transport works fine, so we just
	// honour the timeout.
	wp.newClient = func(cfg Config) *http.Client {
		timeout := cfg.Timeout
		if timeout <= 0 {
			timeout = defaultTimeout
		}
		return &http.Client{Timeout: timeout}
	}
	return wp
}

func sampleRecord() snoozetypes.Record {
	return snoozetypes.Record{
		UID:      "rec-1",
		Host:     "db-1.example.com",
		Source:   "syslog",
		Severity: "warning",
		Message:  "disk full",
	}
}

func TestSendSimplePOST(t *testing.T) {
	var captured struct {
		method      string
		path        string
		contentType string
		body        []byte
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.method = r.Method
		captured.path = r.URL.Path
		captured.contentType = r.Header.Get("Content-Type")
		captured.body, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	p := newPluginForTest(t)
	rec := sampleRecord()
	err := p.Send(context.Background(), rec, plugins.NotificationPayload{
		Meta: map[string]any{
			"url": srv.URL + "/hook",
		},
	})
	require.NoError(t, err)
	require.Equal(t, http.MethodPost, captured.method)
	require.Equal(t, "/hook", captured.path)
	require.Equal(t, "application/json", captured.contentType)

	var got snoozetypes.Record
	require.NoError(t, json.Unmarshal(captured.body, &got))
	require.Equal(t, rec.Host, got.Host)
	require.Equal(t, rec.Severity, got.Severity)
}

func TestSendHeadersAndBodyTemplate(t *testing.T) {
	var captured struct {
		headerX string
		body    string
		query   string
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.headerX = r.Header.Get("X-Host")
		b, _ := io.ReadAll(r.Body)
		captured.body = string(b)
		captured.query = r.URL.RawQuery
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(srv.Close)

	p := newPluginForTest(t)
	rec := sampleRecord()
	err := p.Send(context.Background(), rec, plugins.NotificationPayload{
		Meta: map[string]any{
			"url":    srv.URL + "/h?host={{.Record.Host}}",
			"method": "PUT",
			"headers": map[string]any{
				"X-Host":       "{{.Record.Host}}",
				"X-Static":     "literal",
				"Content-Type": "text/plain",
			},
			"body": "msg={{.Record.Message}} host={{.Record.Host}}",
		},
	})
	require.NoError(t, err)
	require.Equal(t, rec.Host, captured.headerX)
	require.Equal(t, "msg=disk full host="+rec.Host, captured.body)
	require.Equal(t, "host="+rec.Host, captured.query)
}

func TestSendBearerAuth(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	p := newPluginForTest(t)
	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta: map[string]any{
			"url": srv.URL,
			"auth": map[string]any{
				"type":  "bearer",
				"token": "s3cret",
			},
		},
	})
	require.NoError(t, err)
	require.Equal(t, "Bearer s3cret", gotAuth)
}

func TestSendBasicAuth(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	p := newPluginForTest(t)
	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta: map[string]any{
			"url": srv.URL,
			"auth": map[string]any{
				"type":     "basic",
				"username": "alice",
				"password": "wonderland",
			},
		},
	})
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(gotAuth, "Basic "), "got=%q", gotAuth)
	// base64("alice:wonderland") == "YWxpY2U6d29uZGVybGFuZA=="
	require.Equal(t, "Basic YWxpY2U6d29uZGVybGFuZA==", gotAuth)
}

func TestSendErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"nope"}`))
	}))
	t.Cleanup(srv.Close)

	p := newPluginForTest(t)
	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta: map[string]any{"url": srv.URL},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "401")
	require.Contains(t, err.Error(), "nope")
}

func TestSendTimeout(t *testing.T) {
	released := make(chan struct{})

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		select {
		case <-released:
		case <-r.Context().Done():
		}
		w.WriteHeader(http.StatusOK)
	}))
	// Order matters: t.Cleanup is LIFO. The handler blocks until either the
	// request context cancels or `released` closes; closing `released` must
	// happen before srv.Close runs (httptest.Server.Close waits on in-flight
	// handlers).
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(released) })

	p := newPluginForTest(t)
	start := time.Now()
	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta: map[string]any{
			"url":     srv.URL,
			"timeout": "150ms",
		},
	})
	elapsed := time.Since(start)
	require.Error(t, err)
	require.GreaterOrEqual(t, hits.Load(), int32(1))
	require.Less(t, elapsed, 2*time.Second, "timeout should fire well before the default")
}

func TestSendMissingURL(t *testing.T) {
	p := newPluginForTest(t)
	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta: map[string]any{},
	})
	require.Error(t, err)
}

func TestAuthValidation(t *testing.T) {
	p := newPluginForTest(t)
	rec := sampleRecord()
	// bearer without token
	err := p.Send(context.Background(), rec, plugins.NotificationPayload{
		Meta: map[string]any{
			"url":  "http://example.invalid",
			"auth": map[string]any{"type": "bearer"},
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "bearer")

	// unsupported auth type
	err = p.Send(context.Background(), rec, plugins.NotificationPayload{
		Meta: map[string]any{
			"url":  "http://example.invalid",
			"auth": map[string]any{"type": "weird"},
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported auth type")
}

func TestPluginInterfaceContract(t *testing.T) {
	var _ plugins.Plugin = (*Plugin)(nil)
	var _ plugins.Notifier = (*Plugin)(nil)
	p, err := factory(plugins.Metadata{Name: "webhook"})
	require.NoError(t, err)
	require.Equal(t, "webhook", p.Name())
	require.NoError(t, p.PostInit(context.Background(), nil))
	require.NoError(t, p.Reload(context.Background()))
}
