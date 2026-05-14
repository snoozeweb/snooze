package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/japannext/snooze/internal/api/middleware"
	"github.com/japannext/snooze/internal/auth"
	"github.com/japannext/snooze/internal/config/schema"
	"github.com/japannext/snooze/internal/telemetry"
	"github.com/japannext/snooze/pkg/snoozetypes"

	"github.com/prometheus/client_golang/prometheus"
)

// testTokenEngine builds an HS256 token engine with a fresh 32-byte secret.
func testTokenEngine(t *testing.T) *auth.TokenEngine {
	t.Helper()
	cfg := schema.DefaultAuth()
	cfg.TokenLease = schema.Duration(time.Hour)
	eng, err := auth.NewTokenEngine([]byte("00000000000000000000000000000000"), cfg)
	require.NoError(t, err)
	return eng
}

func TestRequestID_MintsWhenAbsent(t *testing.T) {
	var captured string
	h := middleware.RequestID()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = telemetry.RequestIDFrom(r.Context())
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	h.ServeHTTP(rec, req)
	require.NotEmpty(t, captured)
	require.Equal(t, captured, rec.Header().Get(middleware.RequestIDHeader))
}

func TestRequestID_PreservesInbound(t *testing.T) {
	h := middleware.RequestID()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "abc-123", telemetry.RequestIDFrom(r.Context()))
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(middleware.RequestIDHeader, "abc-123")
	h.ServeHTTP(rec, req)
	require.Equal(t, "abc-123", rec.Header().Get(middleware.RequestIDHeader))
}

func TestAuthMiddleware_BearerHappyPath(t *testing.T) {
	eng := testTokenEngine(t)
	tok, _, err := eng.Sign(snoozetypes.Claims{Subject: "alice", Method: "local"})
	require.NoError(t, err)

	var seen snoozetypes.Claims
	h := middleware.Auth(eng, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, ok := auth.ClaimsFrom(r.Context())
		require.True(t, ok)
		seen = c
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "alice", seen.Subject)
}

func TestAuthMiddleware_Missing(t *testing.T) {
	eng := testTokenEngine(t)
	h := middleware.Auth(eng, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("downstream must not be reached")
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	var env snoozetypes.ErrEnvelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	require.Equal(t, "unauthorized", env.Error.Code)
}

func TestAuthMiddleware_BadSignature(t *testing.T) {
	eng := testTokenEngine(t)
	h := middleware.Auth(eng, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("downstream must not be reached")
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer not.a.token")
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestAuthMiddleware_Skip(t *testing.T) {
	eng := testTokenEngine(t)
	reached := false
	skip := func(r *http.Request) bool { return strings.HasPrefix(r.URL.Path, "/healthz") }
	h := middleware.Auth(eng, skip)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	require.True(t, reached)
}

func TestRequirePerm_Allows(t *testing.T) {
	h := middleware.RequirePerm("rw_user")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := auth.WithClaims(req.Context(), snoozetypes.Claims{Permissions: []string{"rw_user"}})
	h.ServeHTTP(rec, req.WithContext(ctx))
	require.Equal(t, http.StatusNoContent, rec.Code)
}

func TestRequirePerm_WildcardAllows(t *testing.T) {
	h := middleware.RequirePerm("rw_anything")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := auth.WithClaims(req.Context(), snoozetypes.Claims{Permissions: []string{"rw_all"}})
	h.ServeHTTP(rec, req.WithContext(ctx))
	require.Equal(t, http.StatusNoContent, rec.Code)
}

func TestRequirePerm_Denies(t *testing.T) {
	h := middleware.RequirePerm("rw_user")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("must not be reached")
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := auth.WithClaims(req.Context(), snoozetypes.Claims{Permissions: []string{"ro_user"}})
	h.ServeHTTP(rec, req.WithContext(ctx))
	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestRequirePerm_NoClaims401(t *testing.T) {
	h := middleware.RequirePerm("rw_user")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("must not be reached")
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestRecoverer_PanicTo500Envelope(t *testing.T) {
	reg := telemetry.NewRegistry(prometheus.NewRegistry())
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	h := middleware.Recoverer(logger, reg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("kaboom")
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	require.Equal(t, http.StatusInternalServerError, rec.Code)
	var env snoozetypes.ErrEnvelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	require.Equal(t, "internal", env.Error.Code)
}

func TestCORS_AllowsAndAnswersPreflight(t *testing.T) {
	cfg := middleware.DefaultCORS()
	h := middleware.CORS(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	// Preflight.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/api/v1/x", nil)
	req.Header.Set("Origin", "https://app.example")
	req.Header.Set("Access-Control-Request-Method", "POST")
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNoContent, rec.Code)
	require.NotEmpty(t, rec.Header().Get("Access-Control-Allow-Methods"))

	// Simple request still passes through.
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.Header.Set("Origin", "https://app.example")
	h.ServeHTTP(rec2, req2)
	require.Equal(t, http.StatusOK, rec2.Code)
	require.Equal(t, "*", rec2.Header().Get("Access-Control-Allow-Origin"))
}

func TestCORS_Disabled(t *testing.T) {
	h := middleware.CORS(middleware.CORSConfig{})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	req.Header.Set("Origin", "https://x")
	h.ServeHTTP(rec, req)
	// With empty AllowOrigins, the middleware is a passthrough.
	require.Equal(t, http.StatusTeapot, rec.Code)
}

func TestAudit_LogsAndSkipsExcluded(t *testing.T) {
	var buf strings.Builder
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	mw := middleware.Audit(logger, []string{"/metrics"})
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	// Excluded path → no log line.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Empty(t, buf.String())

	// Logged path → exactly one entry.
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/api/v1/x", nil))
	require.Contains(t, buf.String(), `"method":"GET"`)
	require.Contains(t, buf.String(), `"path":"/api/v1/x"`)
}
