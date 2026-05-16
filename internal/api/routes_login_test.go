package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// fakeRefresh is a stub refreshIssuer for the login route tests. It records
// every call so the test can assert on rotation / revoke behaviour without a
// real DB driver.
type fakeRefresh struct {
	issuedRaw    string
	issuedExp    time.Time
	rotatedFrom  string
	revoked      []string
	verifyClaims snoozetypes.Claims
	verifyErr    error
	issueErr     error
}

func (f *fakeRefresh) Issue(_ context.Context, c snoozetypes.Claims) (string, time.Time, error) {
	if f.issueErr != nil {
		return "", time.Time{}, f.issueErr
	}
	f.verifyClaims = c
	f.issuedRaw = "refresh-" + c.Subject
	f.issuedExp = time.Now().Add(7 * 24 * time.Hour)
	return f.issuedRaw, f.issuedExp, nil
}

func (f *fakeRefresh) VerifyAndRotate(ctx context.Context, raw string) (snoozetypes.Claims, string, time.Time, error) {
	if f.verifyErr != nil {
		return snoozetypes.Claims{}, "", time.Time{}, f.verifyErr
	}
	f.rotatedFrom = raw
	c := f.verifyClaims
	newRaw, exp, err := f.Issue(ctx, c)
	if err != nil {
		return snoozetypes.Claims{}, "", time.Time{}, err
	}
	return c, newRaw, exp, nil
}

func (f *fakeRefresh) Revoke(_ context.Context, raw string) error {
	f.revoked = append(f.revoked, raw)
	return nil
}

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
		Refresh:   &fakeRefresh{},
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

func TestLoginLocal_IncludesRefreshToken(t *testing.T) {
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
	require.NotEmpty(t, resp.Token, "login must return an access token")
	require.NotEmpty(t, resp.RefreshToken, "login must return a refresh token")
	require.False(t, resp.RefreshExpiresAt.IsZero(), "refresh_expires_at must be populated")
	require.True(t, resp.RefreshExpiresAt.After(resp.ExpiresAt),
		"refresh token must outlive the access token")
}

func TestRefresh_Success(t *testing.T) {
	r, rt := loginTestRouter(t, &fakeProvider{
		name:     "local",
		wantUser: "alice",
		wantPass: "secret",
		enabled:  true,
		identity: auth.Identity{Username: "alice", Method: "local"},
	})

	// First, log in to seed the fake store.
	loginRec := httptest.NewRecorder()
	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/login/local",
		bytes.NewBufferString(`{"username":"alice","password":"secret"}`))
	loginReq.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(loginRec, loginReq)
	require.Equal(t, http.StatusOK, loginRec.Code)
	var first loginResponse
	require.NoError(t, json.Unmarshal(loginRec.Body.Bytes(), &first))
	require.NotEmpty(t, first.RefreshToken)

	// Now exchange that refresh token for a new pair.
	refreshBody := bytes.NewBufferString(`{"refresh_token":"` + first.RefreshToken + `"}`)
	refreshRec := httptest.NewRecorder()
	refreshReq := httptest.NewRequest(http.MethodPost, "/api/v1/login/refresh", refreshBody)
	refreshReq.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(refreshRec, refreshReq)

	require.Equal(t, http.StatusOK, refreshRec.Code)
	var second loginResponse
	require.NoError(t, json.Unmarshal(refreshRec.Body.Bytes(), &second))
	require.NotEmpty(t, second.Token)
	require.NotEmpty(t, second.RefreshToken)
	require.Equal(t, "local", second.Method)
	require.Equal(t, first.RefreshToken,
		rt.Refresh.(*fakeRefresh).rotatedFrom,
		"refresh handler must call VerifyAndRotate on the supplied token")
}

func TestRefresh_InvalidTokenReturns401(t *testing.T) {
	r, rt := loginTestRouter(t)
	rt.Refresh.(*fakeRefresh).verifyErr = errors.New("nope")

	body := bytes.NewBufferString(`{"refresh_token":"bogus"}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/login/refresh", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestRefresh_MissingTokenReturns401(t *testing.T) {
	r, _ := loginTestRouter(t)
	body := bytes.NewBufferString(`{}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/login/refresh", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestLogout_RevokesRefreshToken(t *testing.T) {
	r, rt := loginTestRouter(t)
	body := bytes.NewBufferString(`{"refresh_token":"refresh-alice"}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/login/logout", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Equal(t, []string{"refresh-alice"}, rt.Refresh.(*fakeRefresh).revoked)
}

func TestLogout_EmptyBodyStillSucceeds(t *testing.T) {
	r, rt := loginTestRouter(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/login/logout",
		bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Empty(t, rt.Refresh.(*fakeRefresh).revoked,
		"empty body must be a no-op, not a revoke of \"\"")
}
