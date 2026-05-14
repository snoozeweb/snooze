package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/japannext/snooze/internal/auth"
)

// fakeProvider is a stub auth.Provider for the login route tests.
type fakeProvider struct {
	name      string
	wantUser  string
	wantPass  string
	enabled   bool
	identity  auth.Identity
	failError error
}

func (f *fakeProvider) Name() string { return f.name }
func (f *fakeProvider) Authenticate(_ context.Context, c auth.Credentials) (auth.Identity, error) {
	if !f.enabled {
		return auth.Identity{}, auth.ErrProviderDisabled
	}
	if f.failError != nil {
		return auth.Identity{}, f.failError
	}
	if c.Username == f.wantUser && c.Password == f.wantPass {
		return f.identity, nil
	}
	return auth.Identity{}, auth.ErrInvalidCredentials
}

func loginTestRouter(t *testing.T, providers ...auth.Provider) (chi.Router, *Router) {
	t.Helper()
	reg := auth.NewRegistry()
	for _, p := range providers {
		reg.Register(p)
	}
	rt := &Router{
		Auth:      testTokenEngine(t),
		Providers: reg,
	}
	r := chi.NewRouter()
	rt.mountLogin(r)
	return r, rt
}

func TestLoginLocal_Success(t *testing.T) {
	r, _ := loginTestRouter(t, &fakeProvider{
		name:     "local",
		wantUser: "alice",
		wantPass: "secret",
		enabled:  true,
		identity: auth.Identity{Username: "alice", Method: "local"},
	})

	body := bytes.NewBufferString(`{"username":"alice","password":"secret"}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/login/local", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp loginResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.NotEmpty(t, resp.Token)
	require.Equal(t, "local", resp.Method)
}

func TestLoginLocal_BadCreds(t *testing.T) {
	r, _ := loginTestRouter(t, &fakeProvider{
		name:    "local",
		enabled: true,
	})
	body := bytes.NewBufferString(`{"username":"x","password":"y"}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/login/local", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestLoginLocal_UnknownBackend(t *testing.T) {
	r, _ := loginTestRouter(t /* no providers */)
	body := bytes.NewBufferString(`{"username":"x","password":"y"}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/login/local", body)
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestLoginAnonymous_Enabled(t *testing.T) {
	r, _ := loginTestRouter(t, &fakeProvider{
		name:     "anonymous",
		enabled:  true,
		identity: auth.Identity{Username: "anonymous", Method: "anonymous"},
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/login/anonymous", bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestLoginAnonymous_Disabled(t *testing.T) {
	r, _ := loginTestRouter(t, &fakeProvider{name: "anonymous", enabled: false})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/login/anonymous", bytes.NewBufferString(`{}`))
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusConflict, rec.Code)
}

func TestLoginIndex_ListsBackends(t *testing.T) {
	r, _ := loginTestRouter(t,
		&fakeProvider{name: "local", enabled: true},
		&fakeProvider{name: "ldap", enabled: true},
	)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/login", nil)
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "local")
	require.Contains(t, rec.Body.String(), "ldap")
}
