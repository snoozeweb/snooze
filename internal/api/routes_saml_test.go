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

// fakeSAMLProvider is the SAML analogue of fakeRedirectProvider: it drives the
// route tests without any crypto or live IdP.
type fakeSAMLProvider struct {
	name        string
	enabled     bool
	authURL     string
	identity    auth.Identity
	assertErr   error
	metadataXML []byte
}

func (f *fakeSAMLProvider) Name() string                   { return f.name }
func (f *fakeSAMLProvider) IsEnabled(context.Context) bool { return f.enabled }
func (f *fakeSAMLProvider) DisplayName() string            { return "Corporate SSO" }
func (f *fakeSAMLProvider) Icon() string                   { return "saml" }
func (f *fakeSAMLProvider) Authenticate(context.Context, auth.Credentials) (auth.Identity, error) {
	return auth.Identity{}, auth.ErrRedirectProvider
}
func (f *fakeSAMLProvider) AuthnRequestURL(_ context.Context, relayState string) (string, error) {
	return f.authURL + "?SAMLRequest=req&RelayState=" + relayState, nil
}
func (f *fakeSAMLProvider) ParseAssertion(_ context.Context, _ string) (auth.Identity, error) {
	if f.assertErr != nil {
		return auth.Identity{}, f.assertErr
	}
	return f.identity, nil
}
func (f *fakeSAMLProvider) Metadata(context.Context) ([]byte, error) {
	if f.metadataXML != nil {
		return f.metadataXML, nil
	}
	return []byte(`<EntityDescriptor entityID="https://snooze.example/saml"></EntityDescriptor>`), nil
}

func samlTestRouter(t *testing.T, sp auth.SAMLProvider) (chi.Router, *Router) {
	t.Helper()
	reg := auth.NewRegistry()
	reg.Register(sp)
	rt := &Router{Auth: testTokenEngine(t), Refresh: &fakeRefresh{}, Providers: reg}
	r := chi.NewRouter()
	rt.mountLogin(r)
	return r, rt
}

func samlTestRouterWithDB(t *testing.T, sp auth.SAMLProvider, driver db.Driver) (chi.Router, *Router) {
	t.Helper()
	reg := auth.NewRegistry()
	reg.Register(sp)
	rt := &Router{Auth: testTokenEngine(t), Refresh: &fakeRefresh{}, Providers: reg, DB: driver}
	r := chi.NewRouter()
	rt.mountLogin(r)
	return r, rt
}

func TestSAMLStartRedirects(t *testing.T) {
	sp := &fakeSAMLProvider{name: "saml", enabled: true, authURL: "https://idp.example/sso"}
	r, _ := samlTestRouter(t, sp)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/login/saml/start?return_to=%2Fweb%2Falerts&org=acme", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusFound, w.Code)
	require.True(t, strings.HasPrefix(w.Header().Get("Location"), "https://idp.example/sso?SAMLRequest=req&RelayState="),
		"location: %q", w.Header().Get("Location"))
	cookieStr := strings.Join(w.Header().Values("Set-Cookie"), ";")
	require.Contains(t, cookieStr, samlStateCookie)
	require.Contains(t, cookieStr, "HttpOnly")
	require.Contains(t, cookieStr, "SameSite=Lax")
	require.Contains(t, cookieStr, "Path=/api/v1/login")
}

// samlACS posts a SAMLResponse + RelayState against the ACS route with a state
// cookie carrying the given relayState (so the CSRF compare passes when
// formRelayState == cookieState).
func samlACS(t *testing.T, r chi.Router, rt *Router, cookieState, formRelayState, org string) *httptest.ResponseRecorder {
	t.Helper()
	key := rt.Auth.DeriveKey(samlStateLabel)
	cookie := encodeSAMLState(key, samlState{State: cookieState, ReturnTo: "/web/alerts", Org: org, Exp: 9999999999})
	form := url.Values{}
	form.Set("SAMLResponse", "PHNhbWw+") // base64 of "<saml>" — opaque to the fake
	form.Set("RelayState", formRelayState)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/login/saml/acs", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: samlStateCookie, Value: cookie})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestSAMLACSMintsSession(t *testing.T) {
	store := &userStoreDB{}
	sp := &fakeSAMLProvider{
		name: "saml", enabled: true,
		identity: auth.Identity{Username: "alice@egerie.eu", Method: "saml", Groups: []string{"ops"}},
	}
	r, rt := samlTestRouterWithDB(t, sp, store)

	w := samlACS(t, r, rt, "rs1", "rs1", "")
	require.Equal(t, http.StatusFound, w.Code, "body=%s", w.Body.String())
	loc := w.Header().Get("Location")
	require.True(t, strings.HasPrefix(loc, "/web/login/callback#"), "location: %q", loc)
	vals, err := url.ParseQuery(loc[strings.Index(loc, "#")+1:])
	require.NoError(t, err)
	require.NotEmpty(t, vals.Get("token"), "a session token must be issued")
	require.Equal(t, "/web/alerts", vals.Get("return_to"))

	// JIT row created.
	doc, err := store.GetOne(context.Background(), "user", db.Document{"name": "alice@egerie.eu", "method": "saml"})
	require.NoError(t, err)
	require.Equal(t, true, doc["enabled"])
}

func TestSAMLACSRelayStateMismatch(t *testing.T) {
	sp := &fakeSAMLProvider{
		name: "saml", enabled: true,
		identity: auth.Identity{Username: "alice@egerie.eu", Method: "saml"},
	}
	r, rt := samlTestRouter(t, sp)

	// Cookie says "rs1" but the form echoes "WRONG".
	w := samlACS(t, r, rt, "rs1", "WRONG", "")
	require.Equal(t, http.StatusFound, w.Code)
	require.True(t, strings.HasPrefix(w.Header().Get("Location"), "/web/login?sso_error="),
		"mismatch must redirect to the login-error page, got %q", w.Header().Get("Location"))
	require.Empty(t, rt.Refresh.(*fakeRefresh).issuedRaw, "no token may be minted on RelayState mismatch")
}

func TestSAMLACSDisabledUser(t *testing.T) {
	store := &userStoreDB{}
	store.seedUser(db.Document{
		"name": "carol@egerie.eu", "method": "saml", "tenant_id": "default", "enabled": false,
	})
	sp := &fakeSAMLProvider{
		name: "saml", enabled: true,
		identity: auth.Identity{Username: "carol@egerie.eu", Method: "saml"},
	}
	r, rt := samlTestRouterWithDB(t, sp, store)

	w := samlACS(t, r, rt, "rs1", "rs1", "")
	require.Equal(t, http.StatusFound, w.Code)
	require.True(t, strings.HasPrefix(w.Header().Get("Location"), "/web/login?sso_error="),
		"disabled user must redirect to the login-error page, got %q", w.Header().Get("Location"))
	require.Empty(t, rt.Refresh.(*fakeRefresh).issuedRaw, "no token may be minted for a disabled user")
}

func TestSAMLACSBadAssertion(t *testing.T) {
	sp := &fakeSAMLProvider{
		name: "saml", enabled: true,
		assertErr: auth.ErrInvalidCredentials, // any parse error
	}
	r, rt := samlTestRouter(t, sp)

	w := samlACS(t, r, rt, "rs1", "rs1", "")
	require.Equal(t, http.StatusFound, w.Code)
	require.True(t, strings.HasPrefix(w.Header().Get("Location"), "/web/login?sso_error="),
		"bad assertion must redirect to the login-error page, got %q", w.Header().Get("Location"))
	require.Empty(t, rt.Refresh.(*fakeRefresh).issuedRaw, "no token may be minted on a bad assertion")
}

func TestSAMLMetadataXML(t *testing.T) {
	sp := &fakeSAMLProvider{name: "saml", enabled: true}
	r, _ := samlTestRouter(t, sp)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/login/saml/metadata", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Header().Get("Content-Type"), "xml")
	require.Contains(t, w.Body.String(), "EntityDescriptor")
}
