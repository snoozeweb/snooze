package plugins

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// heartbeatStyleMeta models a plugin whose CRUD is authenticated
// (route_defaults) but whose webhook ping sub-path is public via a per-path
// Routes override — the case per-path resolution must support.
func heartbeatStyleMeta() Metadata {
	return Metadata{
		Name: "heartbeat", PluginName: "heartbeat",
		RouteDefaults: Route{Authentication: boolPtr(true)},
		Routes: map[string]Route{
			"/heartbeat": {
				Authentication:      boolPtr(false),
				AuthorizationPolicy: &AuthorizationPolicy{Write: []string{"any"}},
			},
		},
	}
}

func TestAuthorizeRoute_PublicSubpath_NoClaims_PassesWhileCRUDRequiresAuth(t *testing.T) {
	t.Parallel()
	meta := heartbeatStyleMeta()

	// Public ping sub-path: no claims → allowed.
	ping := AuthorizeRoute(meta, "/heartbeat")(http.HandlerFunc(ok200))
	rec := httptest.NewRecorder()
	ping.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/webhook/heartbeat", nil))
	require.Equal(t, http.StatusOK, rec.Code, "ping sub-path should be public")

	// CRUD path (plugin defaults): no claims → 401.
	crud := AuthorizeRoute(meta, "")(http.HandlerFunc(ok200))
	rec = httptest.NewRecorder()
	crud.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/heartbeat", nil))
	require.Equal(t, http.StatusUnauthorized, rec.Code, "CRUD path should require auth")
}

func TestAuthorizeRoute_PublicSubpath_AuthenticatedUnprivilegedCaller_Passes(t *testing.T) {
	t.Parallel()
	meta := heartbeatStyleMeta()

	mw := AuthorizeRoute(meta, "/heartbeat")(http.HandlerFunc(ok200))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhook/heartbeat", nil)
	req = req.WithContext(auth.WithClaims(req.Context(), snoozetypes.Claims{
		Subject: "viewer", Method: "local", // no permissions at all
	}))
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code,
		"write:[any] on the ping sub-path admits any authenticated caller")
}

func TestAuthorizeCRUD_DelegatesToRouteDefaults(t *testing.T) {
	t.Parallel()
	// AuthorizeCRUD must remain equivalent to AuthorizeRoute(meta, "").
	meta := heartbeatStyleMeta()
	crud := AuthorizeCRUD(meta)(http.HandlerFunc(ok200))
	rec := httptest.NewRecorder()
	crud.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/heartbeat", nil))
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}
