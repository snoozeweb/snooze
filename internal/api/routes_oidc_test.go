package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/db"
)

type fakeRedirectProvider struct {
	name        string
	enabled     bool
	authURL     string
	identity    auth.Identity
	exchangeErr error
}

func (f *fakeRedirectProvider) Name() string                   { return f.name }
func (f *fakeRedirectProvider) IsEnabled(context.Context) bool { return f.enabled }
func (f *fakeRedirectProvider) DisplayName() string            { return "Microsoft 365" }
func (f *fakeRedirectProvider) Icon() string                   { return "microsoft" }
func (f *fakeRedirectProvider) Authenticate(context.Context, auth.Credentials) (auth.Identity, error) {
	return auth.Identity{}, auth.ErrRedirectProvider
}
func (f *fakeRedirectProvider) AuthCodeURL(_ context.Context, state, nonce, _ string) (string, error) {
	return f.authURL + "?state=" + state + "&nonce=" + nonce, nil
}
func (f *fakeRedirectProvider) ExchangeAndVerify(_ context.Context, _, _, _ string) (auth.Identity, error) {
	if f.exchangeErr != nil {
		return auth.Identity{}, f.exchangeErr
	}
	return f.identity, nil
}

func oidcTestRouter(t *testing.T, rp auth.RedirectProvider) (chi.Router, *Router) {
	t.Helper()
	reg := auth.NewRegistry()
	reg.Register(rp)
	rt := &Router{Auth: testTokenEngine(t), Refresh: &fakeRefresh{}, Providers: reg}
	r := chi.NewRouter()
	rt.mountLogin(r)
	return r, rt
}

func oidcTestRouterWithDB(t *testing.T, rp auth.RedirectProvider, driver db.Driver) (chi.Router, *Router) {
	t.Helper()
	reg := auth.NewRegistry()
	reg.Register(rp)
	rt := &Router{Auth: testTokenEngine(t), Refresh: &fakeRefresh{}, Providers: reg, DB: driver}
	r := chi.NewRouter()
	rt.mountLogin(r)
	return r, rt
}

// oidcCallback drives a successful-state callback against the router and returns
// the recorder. State/nonce/verifier match the cookie it sets.
func oidcCallback(t *testing.T, r chi.Router, rt *Router, org string) *httptest.ResponseRecorder {
	t.Helper()
	key := rt.Auth.DeriveKey(oidcStateLabel)
	cookie := encodeOIDCState(key, oidcState{State: "st1", Nonce: "n1", Verifier: "v1", Org: org, Exp: 9999999999})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/login/microsoft/callback?state=st1&code=abc", nil)
	req.AddCookie(&http.Cookie{Name: oidcStateCookie, Value: cookie})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// TestOIDCCallback_JIT_InsertsNewUser verifies a first-time SSO login persists
// an enabled user row (under the default tenant) so it shows on the Users page,
// while still issuing a session token.
func TestOIDCCallback_JIT_InsertsNewUser(t *testing.T) {
	store := &userStoreDB{}
	rp := &fakeRedirectProvider{
		name: "microsoft", enabled: true,
		identity: auth.Identity{Username: "alice@egerie.eu", Method: "microsoft", Groups: []string{"GrafanaAdmin"}},
	}
	r, rt := oidcTestRouterWithDB(t, rp, store)

	w := oidcCallback(t, r, rt, "")
	require.Equal(t, http.StatusFound, w.Code, "body=%s", w.Body.String())
	loc := w.Header().Get("Location")
	require.True(t, strings.HasPrefix(loc, "/web/login/callback#"), "location: %q", loc)
	vals, err := url.ParseQuery(loc[strings.Index(loc, "#")+1:])
	require.NoError(t, err)
	require.NotEmpty(t, vals.Get("token"), "a session token must still be issued")

	doc, err := store.GetOne(context.Background(), "user", db.Document{"name": "alice@egerie.eu", "method": "microsoft"})
	require.NoError(t, err, "JIT user row must exist")
	require.Equal(t, true, doc["enabled"])
	require.Equal(t, "default", doc["tenant_id"])
	require.Equal(t, []string{"GrafanaAdmin"}, doc["groups"])
	require.NotZero(t, doc["last_login"])
}

// TestOIDCCallback_JIT_ReloginPreservesRolesAndUpdatesGroups verifies a repeat
// login refreshes groups/last_login but never clobbers admin-assigned roles or
// the enabled flag, and does not create a duplicate row.
func TestOIDCCallback_JIT_ReloginPreservesRolesAndUpdatesGroups(t *testing.T) {
	store := &userStoreDB{}
	store.seedUser(db.Document{
		"name": "bob@egerie.eu", "method": "microsoft", "tenant_id": "default",
		"enabled": true, "roles": []string{"viewer"}, "groups": []string{"Old"}, "last_login": float64(1),
	})
	rp := &fakeRedirectProvider{
		name: "microsoft", enabled: true,
		identity: auth.Identity{Username: "bob@egerie.eu", Method: "microsoft", Groups: []string{"GrafanaAdmin"}},
	}
	r, rt := oidcTestRouterWithDB(t, rp, store)

	w := oidcCallback(t, r, rt, "")
	require.Equal(t, http.StatusFound, w.Code, "body=%s", w.Body.String())

	doc, err := store.GetOne(context.Background(), "user", db.Document{"name": "bob@egerie.eu", "method": "microsoft"})
	require.NoError(t, err)
	require.Equal(t, []string{"GrafanaAdmin"}, doc["groups"], "groups must be refreshed from the token")
	require.Equal(t, []string{"viewer"}, doc["roles"], "admin-assigned roles must be preserved")
	require.Greater(t, doc["last_login"].(float64), float64(1), "last_login must advance")
	require.Len(t, store.users, 1, "re-login must not create a duplicate row")
}

// TestOIDCCallback_DisabledUser_Blocked verifies a disabled SSO user is rejected
// before any session is minted.
func TestOIDCCallback_DisabledUser_Blocked(t *testing.T) {
	store := &userStoreDB{}
	store.seedUser(db.Document{
		"name": "carol@egerie.eu", "method": "microsoft", "tenant_id": "default", "enabled": false,
	})
	rp := &fakeRedirectProvider{
		name: "microsoft", enabled: true,
		identity: auth.Identity{Username: "carol@egerie.eu", Method: "microsoft", Groups: []string{"GrafanaAdmin"}},
	}
	r, rt := oidcTestRouterWithDB(t, rp, store)

	w := oidcCallback(t, r, rt, "")
	require.Equal(t, http.StatusFound, w.Code)
	require.True(t, strings.HasPrefix(w.Header().Get("Location"), "/web/login?sso_error="),
		"disabled user must be redirected with an error, got %q", w.Header().Get("Location"))
	require.Empty(t, rt.Refresh.(*fakeRefresh).issuedRaw, "no refresh token may be issued for a disabled user")
}

// TestOIDCCallback_JIT_NonDefaultOrg verifies the JIT row is written under the
// state's org tenant, not the default.
func TestOIDCCallback_JIT_NonDefaultOrg(t *testing.T) {
	store := &userStoreDB{}
	rp := &fakeRedirectProvider{
		name: "microsoft", enabled: true,
		identity: auth.Identity{Username: "dan@egerie.eu", Method: "microsoft"},
	}
	r, rt := oidcTestRouterWithDB(t, rp, store)

	w := oidcCallback(t, r, rt, "acme")
	require.Equal(t, http.StatusFound, w.Code, "body=%s", w.Body.String())

	doc, err := store.GetOne(context.Background(), "user", db.Document{"name": "dan@egerie.eu", "method": "microsoft"})
	require.NoError(t, err)
	require.Equal(t, "acme", doc["tenant_id"])
}

func TestOIDCStart_RedirectsWithStateCookie(t *testing.T) {
	rp := &fakeRedirectProvider{name: "microsoft", enabled: true, authURL: "https://login.example/authorize"}
	r, _ := oidcTestRouter(t, rp)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/login/microsoft/start?return_to=%2Fweb%2Falerts&org=acme", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusFound, w.Code)
	require.True(t, strings.HasPrefix(w.Header().Get("Location"), "https://login.example/authorize?state="))
	cookieStr := strings.Join(w.Header().Values("Set-Cookie"), ";")
	require.Contains(t, cookieStr, oidcStateCookie)
	require.Contains(t, cookieStr, "HttpOnly")
	require.Contains(t, cookieStr, "SameSite=Lax")
	require.Contains(t, cookieStr, "Path=/api/v1/login")
}

func TestOIDCCallback_Success(t *testing.T) {
	rp := &fakeRedirectProvider{
		name: "microsoft", enabled: true, authURL: "https://login.example/authorize",
		identity: auth.Identity{Username: "alice@egerie.eu", Method: "microsoft", Groups: []string{"Admin"}},
	}
	r, rt := oidcTestRouter(t, rp)

	key := rt.Auth.DeriveKey(oidcStateLabel)
	cookie := encodeOIDCState(key, oidcState{State: "st1", Nonce: "n1", Verifier: "v1", ReturnTo: "/web/alerts", Exp: 9999999999})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/login/microsoft/callback?state=st1&code=abc", nil)
	req.AddCookie(&http.Cookie{Name: oidcStateCookie, Value: cookie})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusFound, w.Code, "body=%s", w.Body.String())
	loc := w.Header().Get("Location")
	require.True(t, strings.HasPrefix(loc, "/web/login/callback#"), "location: %q", loc)
	frag := loc[strings.Index(loc, "#")+1:]
	vals, err := url.ParseQuery(frag)
	require.NoError(t, err)
	require.NotEmpty(t, vals.Get("token"))
	require.Equal(t, "/web/alerts", vals.Get("return_to"))
	cleared := strings.Join(w.Header().Values("Set-Cookie"), ";")
	require.Contains(t, cleared, oidcStateCookie)
	require.Contains(t, cleared, "Max-Age=0") // cookie cleared (MaxAge=-1 renders as Max-Age=0)
}

func TestOIDCCallback_MissingCookie(t *testing.T) {
	rp := &fakeRedirectProvider{name: "microsoft", enabled: true}
	r, _ := oidcTestRouter(t, rp)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/login/microsoft/callback?state=st1&code=abc", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusFound, w.Code)
	require.True(t, strings.HasPrefix(w.Header().Get("Location"), "/web/login?sso_error="))
}

func TestOIDCCallback_StateMismatch(t *testing.T) {
	rp := &fakeRedirectProvider{name: "microsoft", enabled: true}
	r, rt := oidcTestRouter(t, rp)
	key := rt.Auth.DeriveKey(oidcStateLabel)
	cookie := encodeOIDCState(key, oidcState{State: "st1", Nonce: "n1", Exp: 9999999999})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/login/microsoft/callback?state=WRONG&code=abc", nil)
	req.AddCookie(&http.Cookie{Name: oidcStateCookie, Value: cookie})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusFound, w.Code)
	require.True(t, strings.HasPrefix(w.Header().Get("Location"), "/web/login?sso_error="))
}

func TestOIDCCallback_IdPError(t *testing.T) {
	rp := &fakeRedirectProvider{name: "microsoft", enabled: true}
	r, _ := oidcTestRouter(t, rp)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/login/microsoft/callback?error=access_denied", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.True(t, strings.HasPrefix(w.Header().Get("Location"), "/web/login?sso_error="))
}
