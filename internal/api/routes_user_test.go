package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/db/sqlite"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// newUserTestRouter mounts /api/v1/user/me/* against a fresh sqlite driver
// pre-populated with a single local user "alice" whose password is "secret".
// The chi router is bare — the caller injects auth.Claims into the request
// context manually to simulate the global Auth middleware.
func newUserTestRouter(t *testing.T) (chi.Router, *Router, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "snooze.db")
	drv, err := sqlite.New(context.Background(), sqlite.Config{Path: path})
	require.NoError(t, err)
	t.Cleanup(func() { _ = drv.Close() })

	// Tenant-scoped collections fail-closed without a tenant in ctx; this is a
	// single-tenant test store, so scope the seed write to the default tenant.
	tctx := snoozetypes.WithTenant(context.Background(), snoozetypes.DefaultTenant)
	hash, err := bcrypt.GenerateFromPassword([]byte("secret"), bcrypt.MinCost)
	require.NoError(t, err)
	res, err := drv.Write(tctx, auth.LocalCollection, []db.Document{{
		"name":     "alice",
		"method":   auth.LocalMethod,
		"enabled":  true,
		"password": string(hash),
	}}, db.WriteOptions{Primary: []string{"name", "method"}, UpdateTime: true})
	require.NoError(t, err)
	require.Len(t, res.Added, 1)

	rt := &Router{DB: drv}
	r := chi.NewRouter()
	rt.mountUser(r)
	return r, rt, res.Added[0]
}

func withClaims(req *http.Request, c snoozetypes.Claims) *http.Request {
	// The real Auth middleware sets WithTenant from the verified JWT claim; here
	// we stamp the default tenant so the handler's tenant-scoped DB calls don't
	// fail-closed in this single-tenant test store.
	ctx := snoozetypes.WithTenant(req.Context(), snoozetypes.DefaultTenant)
	return req.WithContext(auth.WithClaims(ctx, c))
}

func TestSelfPasswordChange_Success(t *testing.T) {
	r, rt, uid := newUserTestRouter(t)

	body := bytes.NewBufferString(`{"current_password":"secret","password":"newpass1"}`)
	req := withClaims(
		httptest.NewRequest(http.MethodPost, "/api/v1/user/me/password", body),
		snoozetypes.Claims{Subject: "alice", Method: auth.LocalMethod},
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNoContent, rec.Code, "body=%s", rec.Body.String())

	// New password authenticates; old does not. Tenant-scoped reads need a
	// tenant in ctx (single-tenant test store → default tenant).
	tctx := snoozetypes.WithTenant(context.Background(), snoozetypes.DefaultTenant)
	provider := auth.NewLocalProvider(rt.DB)
	_, err := provider.Authenticate(tctx, auth.Credentials{
		Username: "alice", Password: "newpass1",
	})
	require.NoError(t, err)
	_, err = provider.Authenticate(tctx, auth.Credentials{
		Username: "alice", Password: "secret",
	})
	require.Error(t, err)

	// The stored document was updated by uid (sanity-check).
	doc, err := rt.DB.GetOne(tctx, auth.LocalCollection, db.Document{"uid": uid})
	require.NoError(t, err)
	require.NotEmpty(t, doc["password"])
}

func TestSelfPasswordChange_WrongCurrent(t *testing.T) {
	r, _, _ := newUserTestRouter(t)
	body := bytes.NewBufferString(`{"current_password":"WRONG","password":"newpass1"}`)
	req := withClaims(
		httptest.NewRequest(http.MethodPost, "/api/v1/user/me/password", body),
		snoozetypes.Claims{Subject: "alice", Method: auth.LocalMethod},
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestSelfPasswordChange_NonLocalMethod(t *testing.T) {
	r, _, _ := newUserTestRouter(t)
	body := bytes.NewBufferString(`{"current_password":"secret","password":"newpass1"}`)
	req := withClaims(
		httptest.NewRequest(http.MethodPost, "/api/v1/user/me/password", body),
		snoozetypes.Claims{Subject: "alice", Method: "ldap"},
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestSelfPasswordChange_NoClaims(t *testing.T) {
	r, _, _ := newUserTestRouter(t)
	body := bytes.NewBufferString(`{"current_password":"secret","password":"newpass1"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/user/me/password", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestSelfPasswordChange_EmptyBody(t *testing.T) {
	r, _, _ := newUserTestRouter(t)
	body := bytes.NewBufferString(`{"current_password":"secret","password":""}`)
	req := withClaims(
		httptest.NewRequest(http.MethodPost, "/api/v1/user/me/password", body),
		snoozetypes.Claims{Subject: "alice", Method: auth.LocalMethod},
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)

	var envelope snoozetypes.ErrEnvelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope))
	require.Equal(t, "validation_error", envelope.Error.Code)
}
