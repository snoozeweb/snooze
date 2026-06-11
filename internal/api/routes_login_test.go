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
	"github.com/snoozeweb/snooze/internal/config"
	"github.com/snoozeweb/snooze/internal/config/schema"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// gatedProvider is a fakeProvider that implements auth.EnableChecker so the
// /login backend index can filter it. Authenticate is unused in the tests
// that exercise the filter.
type gatedProvider struct {
	fakeProvider
	visible bool
}

func (g *gatedProvider) IsEnabled(_ context.Context) bool { return g.visible }

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

// TestLoginLocal_DisabledAccountReturns403 verifies that a provider returning
// auth.ErrUserDisabled surfaces as a 403 "account disabled", distinct from the
// generic 401 used for bad credentials.
func TestLoginLocal_DisabledAccountReturns403(t *testing.T) {
	r, _ := loginTestRouter(t, &fakeProvider{
		name:      "local",
		enabled:   true,
		failError: auth.ErrUserDisabled,
	})
	body := bytes.NewBufferString(`{"username":"alice","password":"s3cret"}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/login/local", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code, "body=%s", rec.Body.String())
	require.Contains(t, rec.Body.String(), "account disabled")
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

// TestLoginIndex_FiltersDisabledBackends verifies that providers implementing
// auth.EnableChecker disappear from the /login index when IsEnabled returns
// false. Providers without EnableChecker remain visible.
func TestLoginIndex_FiltersDisabledBackends(t *testing.T) {
	r, _ := loginTestRouter(t,
		&gatedProvider{fakeProvider: fakeProvider{name: "local"}, visible: false},
		&gatedProvider{fakeProvider: fakeProvider{name: "ldap"}, visible: false},
		&gatedProvider{fakeProvider: fakeProvider{name: "anonymous"}, visible: true},
	)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/login", nil)
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	require.Contains(t, body, "anonymous")
	require.NotContains(t, body, `"local"`)
	require.NotContains(t, body, `"ldap"`)
}

// TestLoginAnonymous_AdminGrantsWildcard verifies that the general.anonymous_admin
// flag attaches the admin role + AllPermission wildcard to anonymous sessions.
func TestLoginAnonymous_AdminGrantsWildcard(t *testing.T) {
	r, rt := loginTestRouter(t, &fakeProvider{
		name:     "anonymous",
		enabled:  true,
		identity: auth.Identity{Username: "anonymous", Method: "anonymous"},
	})
	rt.Config = &config.Config{General: schema.General{AnonymousAdmin: true}}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/login/anonymous", bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp loginResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.NotEmpty(t, resp.Token)

	claims, err := rt.Auth.Verify(resp.Token)
	require.NoError(t, err)
	require.Equal(t, []string{"admin"}, claims.Roles)
	require.Equal(t, []string{auth.AllPermission}, claims.Permissions)
}

// TestLoginAnonymous_AdminFlagOff verifies that without anonymous_admin the
// claims still resolve normally (empty roles/perms since there's no DB here).
func TestLoginAnonymous_AdminFlagOff(t *testing.T) {
	r, rt := loginTestRouter(t, &fakeProvider{
		name:     "anonymous",
		enabled:  true,
		identity: auth.Identity{Username: "anonymous", Method: "anonymous"},
	})
	rt.Config = &config.Config{General: schema.General{AnonymousAdmin: false}}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/login/anonymous", bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp loginResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	claims, err := rt.Auth.Verify(resp.Token)
	require.NoError(t, err)
	require.Empty(t, claims.Roles)
	require.Empty(t, claims.Permissions)
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

// TestRefresh_DisabledUserRevokedAnd401 verifies a user disabled after login
// cannot refresh: the rotated token is revoked and the request returns 401.
func TestRefresh_DisabledUserRevokedAnd401(t *testing.T) {
	store := &userStoreDB{}
	store.seedUser(db.Document{
		"name": "alice", "method": "local", "tenant_id": "default", "enabled": false,
	})
	_, rt := loginTestRouter(t, &fakeProvider{name: "local", enabled: true})
	rt.DB = store
	fr := rt.Refresh.(*fakeRefresh)
	fr.verifyClaims = snoozetypes.Claims{Subject: "alice", Method: "local", TenantID: "default"}

	r2 := chi.NewRouter()
	rt.mountLogin(r2)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/login/refresh",
		bytes.NewBufferString(`{"refresh_token":"refresh-alice"}`))
	req.Header.Set("Content-Type", "application/json")
	r2.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code, "body=%s", rec.Body.String())
	require.Contains(t, rec.Body.String(), "account disabled")
	require.NotEmpty(t, fr.revoked, "the rotated refresh token must be revoked")
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

func TestLogin_OrgFieldSetsTenantID(t *testing.T) {
	t.Parallel()
	r, rt := loginTestRouter(t, &fakeProvider{
		name:     "local",
		wantUser: "alice",
		wantPass: "secret",
		enabled:  true,
		identity: auth.Identity{Username: "alice", Method: "local"},
	})

	body := bytes.NewBufferString(`{"username":"alice","password":"secret","org":"acme"}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/login/local", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp loginResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.NotEmpty(t, resp.Token)

	// The access token must carry tenant_id = "acme".
	claims, err := rt.Auth.Verify(resp.Token)
	require.NoError(t, err)
	require.Equal(t, "acme", claims.TenantID, "token must carry the org slug")
}

func TestLogin_OrgFieldOmittedDefaultsToDefaultTenant(t *testing.T) {
	t.Parallel()
	r, rt := loginTestRouter(t, &fakeProvider{
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

	claims, err := rt.Auth.Verify(resp.Token)
	require.NoError(t, err)
	require.Equal(t, snoozetypes.DefaultTenant, claims.TenantID, "omitted org must default to DefaultTenant")
}

func TestLoginAnonymous_OrgFieldSetsTenantID(t *testing.T) {
	t.Parallel()
	r, rt := loginTestRouter(t, &fakeProvider{
		name:     "anonymous",
		enabled:  true,
		identity: auth.Identity{Username: "anonymous", Method: "anonymous"},
	})

	body := bytes.NewBufferString(`{"org":"customers"}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/login/anonymous", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp loginResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	claims, err := rt.Auth.Verify(resp.Token)
	require.NoError(t, err)
	require.Equal(t, "customers", claims.TenantID)
}

// TestHandleLoginIndex_ListsActiveListedTenants verifies that GET /api/v1/login
// includes an active+listed tenants array (never login_key), excludes
// suspended or explicitly unlisted tenants, and treats a missing listed field
// as true.
func TestHandleLoginIndex_ListsActiveListedTenants(t *testing.T) {
	// Build a login router; give it a tenantDB pre-seeded with four docs that
	// exercise every filtering branch.
	tdb := &tenantDB{
		docs: []db.Document{
			{"id": "acme", "display_name": "Acme", "status": "active", "listed": true, "login_key": "secret-key"},
			{"id": "globex", "display_name": "Globex", "status": "active", "listed": false},
			{"id": "initech", "display_name": "Initech", "status": "suspended", "listed": true},
			{"id": "legacy", "display_name": "Legacy", "status": "active"}, // no listed field → treated as listed
		},
	}
	_, rt := loginTestRouter(t, &fakeProvider{name: "local", enabled: true})
	rt.DB = tdb

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/login", nil)
	// Re-mount so the new rt (with DB) handles the request.
	r2 := chi.NewRouter()
	rt.mountLogin(r2)
	r2.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		Data struct {
			Backends []struct {
				Name string `json:"name"`
				Kind string `json:"kind"`
			} `json:"backends"`
			Tenants []struct {
				ID          string `json:"id"`
				DisplayName string `json:"display_name"`
				LoginKey    string `json:"login_key"`
			} `json:"tenants"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	ids := map[string]bool{}
	for _, tn := range resp.Data.Tenants {
		ids[tn.ID] = true
		require.Empty(t, tn.LoginKey, "login_key must never appear in the public list (tenant %s)", tn.ID)
	}
	require.True(t, ids["acme"], "active+listed tenant acme must appear")
	require.True(t, ids["legacy"], "active tenant with no listed field must appear")
	require.False(t, ids["globex"], "explicitly unlisted tenant globex must be excluded")
	require.False(t, ids["initech"], "suspended tenant initech must be excluded")
}

func TestLoginIndex_Descriptors(t *testing.T) {
	local := &fakeProvider{name: "local", enabled: true}
	ms := &fakeRedirectProvider{name: "microsoft", enabled: true}
	r, _ := loginTestRouter(t, local, ms)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/login", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var body struct {
		Data struct {
			Backends []struct {
				Name        string `json:"name"`
				Kind        string `json:"kind"`
				DisplayName string `json:"display_name"`
				Icon        string `json:"icon"`
			} `json:"backends"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))

	byName := map[string]string{}
	disp := map[string]string{}
	icons := map[string]string{}
	for _, b := range body.Data.Backends {
		byName[b.Name] = b.Kind
		disp[b.Name] = b.DisplayName
		icons[b.Name] = b.Icon
	}
	require.Equal(t, "password", byName["local"])
	require.Equal(t, "redirect", byName["microsoft"])
	require.Equal(t, "Microsoft 365", disp["microsoft"])
	require.Equal(t, "microsoft", icons["microsoft"])
}

// TestHandleLoginResolveTenant verifies GET /api/v1/login/tenant?key=<login_key>.
// A valid key for an active tenant resolves to {id, display_name}. An empty
// key, unknown key, or suspended tenant all return a generic 404 — the endpoint
// never resolves by slug, so it cannot be used to enumerate tenants.
func TestHandleLoginResolveTenant(t *testing.T) {
	tdb := &tenantDB{
		docs: []db.Document{
			{"id": "acme", "display_name": "Acme", "status": "active", "login_key": "KEY-acme"},
			{"id": "susp", "display_name": "Susp", "status": "suspended", "login_key": "KEY-susp"},
		},
	}
	_, rt := loginTestRouter(t, &fakeProvider{name: "local", enabled: true})
	rt.DB = tdb

	r2 := chi.NewRouter()
	rt.mountLogin(r2)

	// Valid key resolves to id + display_name.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/login/tenant?key=KEY-acme", nil)
	r2.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "valid key: body=%s", rec.Body.String())

	var resp struct {
		Data struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, "acme", resp.Data.ID)
	require.Equal(t, "Acme", resp.Data.DisplayName)

	// Unknown key, empty key, and missing key all return 404.
	for _, path := range []string{
		"/api/v1/login/tenant?key=NOPE",
		"/api/v1/login/tenant?key=",
		"/api/v1/login/tenant",
	} {
		rec2 := httptest.NewRecorder()
		r2.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusNotFound, rec2.Code, "path %q: want 404, got %d (body=%s)", path, rec2.Code, rec2.Body.String())
	}

	// Suspended tenant returns 404 (not 200).
	rec3 := httptest.NewRecorder()
	r2.ServeHTTP(rec3, httptest.NewRequest(http.MethodGet, "/api/v1/login/tenant?key=KEY-susp", nil))
	require.Equal(t, http.StatusNotFound, rec3.Code, "suspended tenant: want 404, got %d", rec3.Code)

	// Slug cannot be used — querying by id value must not resolve.
	rec4 := httptest.NewRecorder()
	r2.ServeHTTP(rec4, httptest.NewRequest(http.MethodGet, "/api/v1/login/tenant?key=acme", nil))
	require.Equal(t, http.StatusNotFound, rec4.Code, "slug must not resolve (key=acme is not a login_key)")
}

func TestLoginIndex_DefaultBackendFirst(t *testing.T) {
	local := &fakeProvider{name: "local", enabled: true}
	ldap := &fakeProvider{name: "ldap", enabled: true}
	r, rt := loginTestRouter(t, local, ldap)
	rt.Config = &config.Config{}
	rt.Config.General.DefaultAuthBackend = "ldap"

	req := httptest.NewRequest(http.MethodGet, "/api/v1/login", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var body struct {
		Data struct {
			Backends []struct {
				Name string `json:"name"`
			} `json:"backends"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.NotEmpty(t, body.Data.Backends)
	require.Equal(t, "ldap", body.Data.Backends[0].Name, "configured default_auth_backend must be listed first")
}

// tenantGatedProvider is enabled only when the context carries the default
// tenant. It models a RuntimeSettings-backed provider (LDAP/OIDC) whose enabled
// flag lives in the DB under the default tenant.
type tenantGatedProvider struct{ fakeProvider }

func (g *tenantGatedProvider) IsEnabled(ctx context.Context) bool {
	tid, _ := auth.TenantFrom(ctx)
	return tid == snoozetypes.DefaultTenant
}

// TestLoginIndex_RuntimeEnabledSurfacesUnderDefaultTenant verifies the login
// index evaluates provider-enabled under the default tenant, so a backend whose
// enabled state is DB-stored (LDAP/OIDC) appears on the tenant-less login page.
func TestLoginIndex_RuntimeEnabledSurfacesUnderDefaultTenant(t *testing.T) {
	r, _ := loginTestRouter(t, &tenantGatedProvider{fakeProvider: fakeProvider{name: "ldap"}})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/login", nil)
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"ldap"`, "a default-tenant runtime-enabled backend must appear on the login index")
}
