package plugins

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// ok200 is the inner handler the middleware wraps in these tests. Returning
// 200 lets us tell "middleware allowed it" from "middleware blocked it".
func ok200(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }

func TestAuthorizeCRUD_NoClaims_AuthRequired_Returns401(t *testing.T) {
	t.Parallel()
	meta := Metadata{Name: "rule", PluginName: "rule"}
	mw := AuthorizeCRUD(meta)(http.HandlerFunc(ok200))
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/rule", nil))
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestAuthorizeCRUD_NoClaims_AuthDisabled_PassesThrough(t *testing.T) {
	t.Parallel()
	// Webhook-style plugin: authentication off, write open to all.
	meta := Metadata{
		PluginName: "alertmanager",
		Name:       "alertmanager",
		RouteDefaults: Route{
			Authentication:      boolPtr(false),
			AuthorizationPolicy: &AuthorizationPolicy{Write: []string{"any"}},
		},
	}
	mw := AuthorizeCRUD(meta)(http.HandlerFunc(ok200))
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/alertmanager", nil))
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestAuthorizeCRUD_ClaimsWithoutPermission_Returns403(t *testing.T) {
	t.Parallel()
	meta := Metadata{Name: "rule", PluginName: "rule"}
	mw := AuthorizeCRUD(meta)(http.HandlerFunc(ok200))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/rule", nil)
	req = req.WithContext(auth.WithClaims(req.Context(), snoozetypes.Claims{
		Subject: "alice",
		Method:  "local",
		// No permissions at all → not even ro_rule.
	}))
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestAuthorizeCRUD_ClaimsWithMatchingPermission_PassesThrough(t *testing.T) {
	t.Parallel()
	meta := Metadata{Name: "rule", PluginName: "rule"}
	mw := AuthorizeCRUD(meta)(http.HandlerFunc(ok200))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/rule", nil)
	req = req.WithContext(auth.WithClaims(req.Context(), snoozetypes.Claims{
		Subject:     "alice",
		Method:      "local",
		Permissions: []string{"rw_rule"},
	}))
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestAuthorizeCRUD_RootMethodAlwaysPasses(t *testing.T) {
	t.Parallel()
	// Locked-down policy: only `rw_rule` may write. The root JWT issued
	// by the admin socket has method=root and bypasses everything.
	meta := Metadata{
		Name: "rule", PluginName: "rule",
		RouteDefaults: Route{AuthorizationPolicy: &AuthorizationPolicy{Write: []string{"rw_rule"}}},
	}
	mw := AuthorizeCRUD(meta)(http.HandlerFunc(ok200))

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/rule/u1", nil)
	req = req.WithContext(auth.WithClaims(req.Context(), snoozetypes.Claims{
		Subject: "root", Method: "root", Permissions: nil,
	}))
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestAuthorizeCRUD_GetWithReadAnyPolicy_AllowsAuthenticatedNoPermCallers(t *testing.T) {
	t.Parallel()
	// Comment-style: read open to all, write requires can_comment. A user
	// with no permissions at all can GET, cannot POST.
	meta := Metadata{
		Name: "comment", PluginName: "comment",
		RouteDefaults: Route{AuthorizationPolicy: &AuthorizationPolicy{
			Read:  []string{"any"},
			Write: []string{"can_comment"},
		}},
	}
	mw := AuthorizeCRUD(meta)(http.HandlerFunc(ok200))

	get := httptest.NewRequest(http.MethodGet, "/api/v1/comment", nil)
	get = get.WithContext(auth.WithClaims(get.Context(), snoozetypes.Claims{
		Subject: "alice", Method: "local",
	}))
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, get)
	require.Equal(t, http.StatusOK, rec.Code)

	post := httptest.NewRequest(http.MethodPost, "/api/v1/comment", nil)
	post = post.WithContext(auth.WithClaims(post.Context(), snoozetypes.Claims{
		Subject: "alice", Method: "local",
	}))
	rec = httptest.NewRecorder()
	mw.ServeHTTP(rec, post)
	require.Equal(t, http.StatusForbidden, rec.Code)
}
