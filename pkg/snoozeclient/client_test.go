package snoozeclient_test

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/japannext/snooze/pkg/snoozeclient"
	"github.com/japannext/snooze/pkg/snoozetypes"
)

// fastOpts returns a minimally-configured Options that points at srv and uses
// a sub-millisecond initial backoff so retry tests don't sit on the wall clock.
func fastOpts(t *testing.T, srv *httptest.Server) snoozeclient.Options {
	t.Helper()
	dir := t.TempDir()
	return snoozeclient.Options{
		BaseURL:        srv.URL,
		Username:       "alice",
		Password:       "hunter2",
		TokenCacheFile: filepath.Join(dir, "token"),
		Timeout:        2 * time.Second,
		MaxRetries:     3,
		InitialBackoff: time.Millisecond,
		HTTPClient:     srv.Client(),
	}
}

// writeJSON is a tiny helper that mirrors the server's render.WriteJSON.
func writeJSON(t *testing.T, w http.ResponseWriter, status int, body any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	require.NoError(t, json.NewEncoder(w).Encode(body))
}

// errorBody helps emit the canonical error envelope.
func errorBody(code, msg string) snoozetypes.ErrEnvelope {
	return snoozetypes.ErrEnvelope{Error: snoozetypes.ErrBody{Code: code, Message: msg}}
}

func TestNewValidation(t *testing.T) {
	t.Run("requires BaseURL", func(t *testing.T) {
		_, err := snoozeclient.New(snoozeclient.Options{})
		require.ErrorIs(t, err, snoozeclient.ErrMissingBaseURL)
	})
	t.Run("trims trailing slash and seeds cache token", func(t *testing.T) {
		dir := t.TempDir()
		cache := filepath.Join(dir, "tok")
		require.NoError(t, os.WriteFile(cache, []byte("cached-tok\n"), 0o600))
		c, err := snoozeclient.New(snoozeclient.Options{
			BaseURL:        "https://example.com/",
			TokenCacheFile: cache,
			Username:       "u",
			Password:       "p",
		})
		require.NoError(t, err)
		require.Equal(t, "https://example.com", c.BaseURL())
		require.Equal(t, "cached-tok", c.Token())
	})
	t.Run("explicit token bypasses cache", func(t *testing.T) {
		c, err := snoozeclient.New(snoozeclient.Options{
			BaseURL: "https://example.com",
			Token:   "from-opts",
		})
		require.NoError(t, err)
		require.Equal(t, "from-opts", c.Token())
	})
}

func TestLogin(t *testing.T) {
	t.Run("happy path stores token in memory and on disk", func(t *testing.T) {
		var capturedBody loginRequestCapture
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, "/api/v1/login/local", r.URL.Path)
			require.NoError(t, json.NewDecoder(r.Body).Decode(&capturedBody))
			writeJSON(t, w, http.StatusOK, map[string]any{
				"token":      "secret-token",
				"expires_at": time.Now().Add(time.Hour),
				"method":     "local",
			})
		}))
		defer srv.Close()
		opts := fastOpts(t, srv)
		c, err := snoozeclient.New(opts)
		require.NoError(t, err)
		require.NoError(t, c.Login(context.Background()))
		require.Equal(t, "secret-token", c.Token())
		require.Equal(t, "alice", capturedBody.Username)
		require.Equal(t, "hunter2", capturedBody.Password)
		raw, err := os.ReadFile(opts.TokenCacheFile)
		require.NoError(t, err)
		require.Equal(t, "secret-token", strings.TrimSpace(string(raw)))
		// Cache permissions must be 0600.
		info, err := os.Stat(opts.TokenCacheFile)
		require.NoError(t, err)
		require.EqualValues(t, 0o600, info.Mode().Perm())
	})
	t.Run("bad credentials surface as APIError", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(t, w, http.StatusUnauthorized, errorBody("unauthorized", "invalid credentials"))
		}))
		defer srv.Close()
		c, err := snoozeclient.New(fastOpts(t, srv))
		require.NoError(t, err)
		err = c.Login(context.Background())
		require.Error(t, err)
		apiErr, ok := snoozeclient.IsAPIError(err)
		require.True(t, ok)
		require.Equal(t, http.StatusUnauthorized, apiErr.Status)
		require.Equal(t, "unauthorized", apiErr.Code)
	})
	t.Run("5xx retries until success", func(t *testing.T) {
		var calls atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if calls.Add(1) == 1 {
				writeJSON(t, w, http.StatusBadGateway, errorBody("upstream", "boom"))
				return
			}
			writeJSON(t, w, http.StatusOK, map[string]any{"token": "ok-tok"})
		}))
		defer srv.Close()
		c, err := snoozeclient.New(fastOpts(t, srv))
		require.NoError(t, err)
		require.NoError(t, c.Login(context.Background()))
		require.EqualValues(t, 2, calls.Load())
		require.Equal(t, "ok-tok", c.Token())
	})
	t.Run("requires Username unless anonymous", func(t *testing.T) {
		c, err := snoozeclient.New(snoozeclient.Options{BaseURL: "http://example.com"})
		require.NoError(t, err)
		err = c.Login(context.Background())
		require.Error(t, err)
		require.Contains(t, err.Error(), "Username is required")
	})
}

// loginRequestCapture is a private mirror of the wire shape.
type loginRequestCapture struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func TestDoGet(t *testing.T) {
	t.Run("happy path with token header", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, "Bearer initial-tok", r.Header.Get("Authorization"))
			require.Equal(t, "/api/v1/things/42", r.URL.Path)
			writeJSON(t, w, http.StatusOK, map[string]any{"name": "thing-42"})
		}))
		defer srv.Close()
		opts := fastOpts(t, srv)
		opts.Token = "initial-tok"
		c, err := snoozeclient.New(opts)
		require.NoError(t, err)
		var got struct {
			Name string `json:"name"`
		}
		require.NoError(t, c.Get(context.Background(), "/api/v1/things/42", &got))
		require.Equal(t, "thing-42", got.Name)
	})
	t.Run("dest=nil discards body", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(t, w, http.StatusOK, map[string]any{"name": "x"})
		}))
		defer srv.Close()
		c, err := snoozeclient.New(fastOpts(t, srv))
		require.NoError(t, err)
		require.NoError(t, c.Get(context.Background(), "/api/v1/things", nil))
	})
	t.Run("4xx returns APIError without retry", func(t *testing.T) {
		var calls atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			calls.Add(1)
			writeJSON(t, w, http.StatusBadRequest, snoozetypes.ErrEnvelope{
				Error: snoozetypes.ErrBody{
					Code:      "bad_request",
					Message:   "missing field",
					Details:   map[string]any{"field": "host"},
					RequestID: "req-1",
					TraceID:   "trace-2",
				},
			})
		}))
		defer srv.Close()
		c, err := snoozeclient.New(fastOpts(t, srv))
		require.NoError(t, err)
		err = c.Get(context.Background(), "/api/v1/things", nil)
		require.Error(t, err)
		apiErr, ok := snoozeclient.IsAPIError(err)
		require.True(t, ok)
		require.Equal(t, http.StatusBadRequest, apiErr.Status)
		require.Equal(t, "bad_request", apiErr.Code)
		require.Equal(t, "missing field", apiErr.Message)
		require.Equal(t, "host", apiErr.Details["field"])
		require.Equal(t, "req-1", apiErr.RequestID)
		require.Equal(t, "trace-2", apiErr.TraceID)
		require.EqualValues(t, 1, calls.Load(), "4xx must not retry")
	})
	t.Run("5xx retries then succeeds", func(t *testing.T) {
		var calls atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			n := calls.Add(1)
			if n == 1 {
				writeJSON(t, w, http.StatusInternalServerError, errorBody("internal", "kaboom"))
				return
			}
			writeJSON(t, w, http.StatusOK, map[string]any{"ok": true})
		}))
		defer srv.Close()
		c, err := snoozeclient.New(fastOpts(t, srv))
		require.NoError(t, err)
		var got map[string]bool
		require.NoError(t, c.Get(context.Background(), "/api/v1/health", &got))
		require.True(t, got["ok"])
		require.EqualValues(t, 2, calls.Load())
	})
	t.Run("5xx exhausts retries and returns APIError", func(t *testing.T) {
		var calls atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			calls.Add(1)
			writeJSON(t, w, http.StatusServiceUnavailable, errorBody("unavailable", "down"))
		}))
		defer srv.Close()
		opts := fastOpts(t, srv)
		opts.MaxRetries = 2
		c, err := snoozeclient.New(opts)
		require.NoError(t, err)
		err = c.Get(context.Background(), "/api/v1/things", nil)
		require.Error(t, err)
		apiErr, ok := snoozeclient.IsAPIError(err)
		require.True(t, ok)
		require.Equal(t, http.StatusServiceUnavailable, apiErr.Status)
		// initial call + 2 retries == 3 total
		require.EqualValues(t, 3, calls.Load())
	})
}

func TestAutoLoginOn401(t *testing.T) {
	t.Run("re-logs in and retries once", func(t *testing.T) {
		var (
			thingCalls atomic.Int32
			loginCalls atomic.Int32
		)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/v1/login/local":
				loginCalls.Add(1)
				writeJSON(t, w, http.StatusOK, map[string]any{"token": "fresh-token"})
			case "/api/v1/things":
				n := thingCalls.Add(1)
				if n == 1 {
					require.Equal(t, "Bearer stale", r.Header.Get("Authorization"))
					writeJSON(t, w, http.StatusUnauthorized, errorBody("unauthorized", "expired"))
					return
				}
				require.Equal(t, "Bearer fresh-token", r.Header.Get("Authorization"))
				writeJSON(t, w, http.StatusOK, map[string]any{"ok": true})
			default:
				http.NotFound(w, r)
			}
		}))
		defer srv.Close()
		opts := fastOpts(t, srv)
		opts.Token = "stale"
		c, err := snoozeclient.New(opts)
		require.NoError(t, err)
		var got map[string]bool
		require.NoError(t, c.Get(context.Background(), "/api/v1/things", &got))
		require.True(t, got["ok"])
		require.EqualValues(t, 1, loginCalls.Load())
		require.EqualValues(t, 2, thingCalls.Load())
		require.Equal(t, "fresh-token", c.Token())
	})
	t.Run("relogin fails, surfaces error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/v1/login/local" {
				writeJSON(t, w, http.StatusUnauthorized, errorBody("unauthorized", "bad creds"))
				return
			}
			writeJSON(t, w, http.StatusUnauthorized, errorBody("unauthorized", "expired"))
		}))
		defer srv.Close()
		opts := fastOpts(t, srv)
		opts.Token = "stale"
		c, err := snoozeclient.New(opts)
		require.NoError(t, err)
		err = c.Get(context.Background(), "/api/v1/things", nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "re-login after 401")
	})
	t.Run("no relogin when credentials are absent", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(t, w, http.StatusUnauthorized, errorBody("unauthorized", "expired"))
		}))
		defer srv.Close()
		// No Username / Password — only a token.
		opts := snoozeclient.Options{
			BaseURL:        srv.URL,
			Token:          "stale",
			TokenCacheFile: filepath.Join(t.TempDir(), "tok"),
			InitialBackoff: time.Millisecond,
			HTTPClient:     srv.Client(),
		}
		c, err := snoozeclient.New(opts)
		require.NoError(t, err)
		err = c.Get(context.Background(), "/api/v1/things", nil)
		require.Error(t, err)
		apiErr, ok := snoozeclient.IsAPIError(err)
		require.True(t, ok)
		require.Equal(t, http.StatusUnauthorized, apiErr.Status)
	})
}

func TestPostAlert(t *testing.T) {
	t.Run("happy path round-trips a record", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, http.MethodPost, r.Method)
			require.Equal(t, "/api/v1/alerts", r.URL.Path)
			var incoming snoozetypes.Record
			require.NoError(t, json.NewDecoder(r.Body).Decode(&incoming))
			require.Equal(t, "db-1", incoming.Host)
			writeJSON(t, w, http.StatusOK, map[string]any{
				"data": []map[string]any{{
					"uid":      "u-1",
					"host":     incoming.Host,
					"severity": "warning",
					"message":  "disk full",
				}},
			})
		}))
		defer srv.Close()
		opts := fastOpts(t, srv)
		opts.Token = "tok"
		c, err := snoozeclient.New(opts)
		require.NoError(t, err)
		rec, err := c.PostAlert(context.Background(), snoozetypes.Record{
			Host: "db-1", Severity: "warning", Message: "disk full",
		})
		require.NoError(t, err)
		require.Equal(t, "u-1", rec.UID)
		require.Equal(t, "db-1", rec.Host)
	})
	t.Run("server-reported error surfaces as error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(t, w, http.StatusOK, map[string]any{
				"data":   []map[string]any{},
				"errors": []string{"plugin rejected: missing host"},
			})
		}))
		defer srv.Close()
		opts := fastOpts(t, srv)
		opts.Token = "tok"
		c, err := snoozeclient.New(opts)
		require.NoError(t, err)
		_, err = c.PostAlert(context.Background(), snoozetypes.Record{})
		require.Error(t, err)
		require.Contains(t, err.Error(), "plugin rejected")
	})
}

func TestTokenCacheRoundtrip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, http.StatusOK, map[string]any{"token": "persisted-tok"})
	}))
	defer srv.Close()
	opts := fastOpts(t, srv)
	c, err := snoozeclient.New(opts)
	require.NoError(t, err)
	require.NoError(t, c.Login(context.Background()))
	// Build a fresh client pointed at the same cache file — it should pick up
	// the persisted token without calling Login.
	opts2 := snoozeclient.Options{
		BaseURL:        srv.URL,
		Username:       "alice",
		Password:       "hunter2",
		TokenCacheFile: opts.TokenCacheFile,
		HTTPClient:     srv.Client(),
	}
	c2, err := snoozeclient.New(opts2)
	require.NoError(t, err)
	require.Equal(t, "persisted-tok", c2.Token())
	// Logout removes the file.
	require.NoError(t, c2.Logout())
	_, err = os.Stat(opts.TokenCacheFile)
	require.True(t, errors.Is(err, os.ErrNotExist))
	require.Empty(t, c2.Token())
}

func TestInsecureTLS(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, http.StatusOK, map[string]any{"ok": true})
	}))
	defer srv.Close()
	// We can't use srv.Client() (which already trusts the cert) — that would
	// not exercise the Insecure flag. Instead, point at the URL and rely on
	// the SDK to build its own transport.
	opts := snoozeclient.Options{
		BaseURL:        srv.URL,
		Token:          "tok",
		Insecure:       true,
		Timeout:        2 * time.Second,
		MaxRetries:     1,
		InitialBackoff: time.Millisecond,
		TokenCacheFile: filepath.Join(t.TempDir(), "tok"),
	}
	c, err := snoozeclient.New(opts)
	require.NoError(t, err)
	var got map[string]bool
	require.NoError(t, c.Get(context.Background(), "/", &got))
	require.True(t, got["ok"])

	// Smoke test the inverse: with Insecure=false the call must fail with a TLS error.
	opts.Insecure = false
	c2, err := snoozeclient.New(opts)
	require.NoError(t, err)
	err = c2.Get(context.Background(), "/", &got)
	require.Error(t, err)
	// Accept either a TLS or x509 error string; both indicate verification failed.
	require.True(t, isTLSError(err), "unexpected error: %v", err)
}

func isTLSError(err error) bool {
	if err == nil {
		return false
	}
	// Walk url.Error → underlying.
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	if _, ok := err.(*tls.CertificateVerificationError); ok {
		return true
	}
	s := err.Error()
	return strings.Contains(s, "x509") || strings.Contains(s, "certificate") || strings.Contains(s, "tls:")
}

// TestDoMethodsCoverMutators exercises Post/Put/Patch/Delete in a single
// table-driven case so the helpers don't bit-rot.
func TestDoMethodsCoverMutators(t *testing.T) {
	cases := []struct {
		name   string
		method string
		exec   func(c *snoozeclient.Client, ctx context.Context) error
	}{
		{"Post", http.MethodPost, func(c *snoozeclient.Client, ctx context.Context) error {
			return c.Post(ctx, "/api/v1/x", map[string]string{"a": "b"}, nil)
		}},
		{"Put", http.MethodPut, func(c *snoozeclient.Client, ctx context.Context) error {
			return c.Put(ctx, "/api/v1/x", map[string]string{"a": "b"}, nil)
		}},
		{"Patch", http.MethodPatch, func(c *snoozeclient.Client, ctx context.Context) error {
			return c.Patch(ctx, "/api/v1/x", map[string]string{"a": "b"}, nil)
		}},
		{"Delete", http.MethodDelete, func(c *snoozeclient.Client, ctx context.Context) error {
			return c.Delete(ctx, "/api/v1/x", nil)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, tc.method, r.Method)
				// Drain so the connection can be reused.
				_, _ = io.Copy(io.Discard, r.Body)
				w.WriteHeader(http.StatusNoContent)
			}))
			defer srv.Close()
			opts := fastOpts(t, srv)
			opts.Token = "tok"
			c, err := snoozeclient.New(opts)
			require.NoError(t, err)
			require.NoError(t, tc.exec(c, context.Background()))
		})
	}
}
