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
