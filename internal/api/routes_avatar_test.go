package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image/png"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/db/sqlite"
	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// peopleHarness mounts the self-service user routes and the people/avatar
// directory against a fresh SQLite store holding users in two tenants. The
// chi router is bare; callers attach claims with peopleReq, the way the global
// Auth middleware would.
func peopleHarness(t *testing.T) (chi.Router, db.Driver) {
	t.Helper()
	d, err := sqlite.New(context.Background(), sqlite.Config{Path: filepath.Join(t.TempDir(), "snooze.db")})
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })

	opts := db.WriteOptions{Primary: []string{"name", "method"}, UpdateTime: true}
	def := snoozetypes.WithTenant(context.Background(), snoozetypes.DefaultTenant)
	_, err = d.Write(def, auth.LocalCollection, []db.Document{
		{"name": "alice", "method": "local", "enabled": true, "display_name": "Alice Martin",
			"password": "hash", "roles": []any{"admin"}, "email": "alice@example.com"},
		{"name": "bob", "method": "ldap", "enabled": true, "display_name": "bob Durand", "groups": []any{"ops"}},
		{"name": "carol", "method": "local", "enabled": false, "display_name": "Carol"},
		// No `enabled` key: enabled, like at the login path.
		{"name": "zoe", "method": "oidc"},
		// Same login under another method: a distinct person.
		{"name": "alice", "method": "oidc", "enabled": true, "display_name": "Alice Martin"},
	}, opts)
	require.NoError(t, err)
	acme := snoozetypes.WithTenant(context.Background(), "acme")
	_, err = d.Write(acme, auth.LocalCollection, []db.Document{
		{"name": "eve", "method": "local", "enabled": true, "display_name": "Eve"},
	}, opts)
	require.NoError(t, err)

	rt := &Router{DB: d}
	r := chi.NewRouter()
	rt.mountUser(r)
	rt.mountPeople(r)
	return r, d
}

// peopleReq issues a request as user/method in tenant (claims + tenant ctx,
// mirroring the Auth middleware). An empty user sends no claims at all.
func peopleReq(t *testing.T, r chi.Router, method, target string, body any, tenant, user, userMethod string) *httptest.ResponseRecorder {
	t.Helper()
	var buf []byte
	if body != nil {
		var err error
		buf, err = json.Marshal(body)
		require.NoError(t, err)
	}
	req := httptest.NewRequest(method, target, bytes.NewReader(buf))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	ctx := snoozetypes.WithTenant(req.Context(), tenant)
	if user != "" {
		ctx = auth.WithClaims(ctx, snoozetypes.Claims{Subject: user, Method: userMethod, TenantID: tenant})
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req.WithContext(ctx))
	return rec
}

func listPeople(t *testing.T, r chi.Router, tenant, user string) []map[string]any {
	t.Helper()
	rec := peopleReq(t, r, http.MethodGet, "/api/v1/people", nil, tenant, user, "local")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var env struct {
		Data []map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	return env.Data
}

func TestPeople_EnabledTenantUsersSortedByDisplayName(t *testing.T) {
	t.Parallel()
	r, _ := peopleHarness(t)

	people := listPeople(t, r, snoozetypes.DefaultTenant, "alice")
	var got []string
	for _, p := range people {
		got = append(got, p["name"].(string)+"/"+p["method"].(string))
		// Directory entries carry no roles, groups, email or secrets.
		for k := range p {
			require.Contains(t, []string{"name", "method", "display_name", "avatar_version"}, k)
		}
	}
	// Case-insensitive by display name (falling back to the login), then login,
	// then method; carol is disabled and eve belongs to another tenant.
	require.Equal(t, []string{"alice/local", "alice/oidc", "bob/ldap", "zoe/oidc"}, got)
	require.Equal(t, "Alice Martin", people[0]["display_name"])
	require.Equal(t, "", people[3]["display_name"])
	require.Equal(t, "", people[0]["avatar_version"])

	acme := listPeople(t, r, "acme", "eve")
	require.Len(t, acme, 1)
	require.Equal(t, "eve", acme[0]["name"])
}

func TestPeople_RequiresAuthentication(t *testing.T) {
	t.Parallel()
	r, _ := peopleHarness(t)
	rec := peopleReq(t, r, http.MethodGet, "/api/v1/people", nil, snoozetypes.DefaultTenant, "", "")
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func avatarBody(t *testing.T, w, h int) map[string]any {
	t.Helper()
	return map[string]any{"data": dataURL("image/png", encodePNG(t, solidImage(w, h)))}
}

func getAvatar(t *testing.T, r chi.Router, tenant, method, name string) (int, map[string]any) {
	t.Helper()
	rec := peopleReq(t, r, http.MethodGet, "/api/v1/avatar/"+method+"/"+name, nil, tenant, "bob", "ldap")
	var out map[string]any
	if rec.Code == http.StatusOK {
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	}
	return rec.Code, out
}

func userAvatarVersion(t *testing.T, d db.Driver, name, method string) any {
	t.Helper()
	ctx := snoozetypes.WithTenant(context.Background(), snoozetypes.DefaultTenant)
	doc, err := d.GetOne(ctx, auth.LocalCollection, db.Document{"name": name, "method": method})
	require.NoError(t, err)
	return doc["avatar_version"]
}

func TestAvatar_RoundTrip(t *testing.T) {
	t.Parallel()
	r, d := peopleHarness(t)
	def := snoozetypes.DefaultTenant

	code, _ := getAvatar(t, r, def, "local", "alice")
	require.Equal(t, http.StatusNotFound, code, "no picture yet")

	rec := peopleReq(t, r, http.MethodPut, "/api/v1/user/me/avatar", avatarBody(t, 64, 64), def, "alice", "local")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var put struct {
		Version string `json:"version"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &put))
	require.Len(t, put.Version, 16)

	// Readable by any authenticated user of the tenant, as a PNG data URL.
	code, got := getAvatar(t, r, def, "local", "alice")
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, "alice", got["name"])
	require.Equal(t, "local", got["method"])
	require.Equal(t, put.Version, got["version"])
	url, _ := got["data"].(string)
	payload, ok := strings.CutPrefix(url, "data:image/png;base64,")
	require.True(t, ok, "data must be a PNG data URL, got %.40q", url)
	raw, err := base64.StdEncoding.DecodeString(payload)
	require.NoError(t, err)
	cfg, err := png.DecodeConfig(bytes.NewReader(raw))
	require.NoError(t, err)
	require.Equal(t, 64, cfg.Width)

	// Stored doc keeps the bare base64 (no data: prefix).
	ctx := snoozetypes.WithTenant(context.Background(), def)
	doc, err := d.GetOne(ctx, avatarCollection, db.Document{"name": "alice", "method": "local"})
	require.NoError(t, err)
	require.Equal(t, payload, doc["data"])
	require.NotZero(t, doc["updated_epoch"])

	// The version is mirrored on the user doc and surfaces in the directory.
	require.Equal(t, put.Version, userAvatarVersion(t, d, "alice", "local"))
	require.Nil(t, userAvatarVersion(t, d, "alice", "oidc"), "the same login under another method is untouched")
	people := listPeople(t, r, def, "bob")
	require.Equal(t, put.Version, people[0]["avatar_version"])

	// A different picture replaces the stored one and changes the version.
	rec = peopleReq(t, r, http.MethodPut, "/api/v1/user/me/avatar", avatarBody(t, 32, 32), def, "alice", "local")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var put2 struct {
		Version string `json:"version"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &put2))
	require.NotEqual(t, put.Version, put2.Version)
	code, got = getAvatar(t, r, def, "local", "alice")
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, put2.Version, got["version"])
	require.Equal(t, put2.Version, userAvatarVersion(t, d, "alice", "local"))
	docs, _, err := d.Search(ctx, avatarCollection, condition.Cond{}, db.Page{})
	require.NoError(t, err)
	require.Len(t, docs, 1, "re-upload upserts on (name, method)")

	// Delete: 204, then 404, version cleared; a second delete is still 204.
	rec = peopleReq(t, r, http.MethodDelete, "/api/v1/user/me/avatar", nil, def, "alice", "local")
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	code, _ = getAvatar(t, r, def, "local", "alice")
	require.Equal(t, http.StatusNotFound, code)
	require.Equal(t, "", userAvatarVersion(t, d, "alice", "local"))
	rec = peopleReq(t, r, http.MethodDelete, "/api/v1/user/me/avatar", nil, def, "alice", "local")
	require.Equal(t, http.StatusNoContent, rec.Code)
}

// TestAvatar_SelfOnly proves the upload always lands on the caller's own
// identity: bob's upload never becomes alice's picture, whatever the body says.
func TestAvatar_SelfOnly(t *testing.T) {
	t.Parallel()
	r, d := peopleHarness(t)
	def := snoozetypes.DefaultTenant
	body := avatarBody(t, 16, 16)
	body["name"] = "alice"
	body["method"] = "local"
	rec := peopleReq(t, r, http.MethodPut, "/api/v1/user/me/avatar", body, def, "bob", "ldap")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	code, _ := getAvatar(t, r, def, "local", "alice")
	require.Equal(t, http.StatusNotFound, code)
	code, got := getAvatar(t, r, def, "ldap", "bob")
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, "bob", got["name"])
	require.Nil(t, userAvatarVersion(t, d, "alice", "local"))
}

func TestAvatar_TenantScoped(t *testing.T) {
	t.Parallel()
	r, _ := peopleHarness(t)
	rec := peopleReq(t, r, http.MethodPut, "/api/v1/user/me/avatar", avatarBody(t, 16, 16), snoozetypes.DefaultTenant, "alice", "local")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	code, _ := getAvatar(t, r, "acme", "local", "alice")
	require.Equal(t, http.StatusNotFound, code, "another tenant must not see the picture")
}

func TestAvatar_AuthRules(t *testing.T) {
	t.Parallel()
	r, _ := peopleHarness(t)
	def := snoozetypes.DefaultTenant

	for _, method := range []string{http.MethodPut, http.MethodDelete} {
		var body any
		if method == http.MethodPut {
			body = avatarBody(t, 16, 16)
		}
		rec := peopleReq(t, r, method, "/api/v1/user/me/avatar", body, def, "", "")
		require.Equal(t, http.StatusUnauthorized, rec.Code, "%s without claims", method)

		// An API key authenticates as its owner but carries method "apikey",
		// not the owner's auth method, so it cannot name a picture's identity.
		rec = peopleReq(t, r, method, "/api/v1/user/me/avatar", body, def, "alice", auth.APIKeyMethod)
		require.Equal(t, http.StatusForbidden, rec.Code, "%s with an API key", method)
	}

	rec := peopleReq(t, r, http.MethodGet, "/api/v1/avatar/local/alice", nil, def, "", "")
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestAvatar_UploadValidation(t *testing.T) {
	t.Parallel()
	r, _ := peopleHarness(t)
	def := snoozetypes.DefaultTenant
	put := func(body any) *httptest.ResponseRecorder {
		return peopleReq(t, r, http.MethodPut, "/api/v1/user/me/avatar", body, def, "alice", "local")
	}

	require.Equal(t, http.StatusUnprocessableEntity, put(map[string]any{
		"data": dataURL("image/svg+xml", []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`)),
	}).Code)
	require.Equal(t, http.StatusUnprocessableEntity, put(avatarBody(t, 100, 300)).Code, "too tall")
	require.Equal(t, http.StatusUnprocessableEntity, put(map[string]any{"data": ""}).Code, "missing data")
	require.Equal(t, http.StatusRequestEntityTooLarge, put(map[string]any{
		"data": dataURL("image/png", encodePNG(t, noiseImage(512, 512))),
	}).Code, "input over the decoded limit")
	// A body far past any legal upload is cut off before it is buffered.
	require.Equal(t, http.StatusRequestEntityTooLarge, put(map[string]any{
		"data": "data:image/png;base64," + strings.Repeat("A", 4<<20),
	}).Code, "oversized request body")

	code, _ := getAvatar(t, r, def, "local", "alice")
	require.Equal(t, http.StatusNotFound, code, "rejected uploads store nothing")
}

// TestAvatar_MeRouteWinsOverUserCRUD mounts the user plugin's generic CRUD
// beside mountUser, in router.go's order, and checks that /user/me/avatar still
// reaches the self-service handler rather than the /{uid} matcher.
func TestAvatar_MeRouteWinsOverUserCRUD(t *testing.T) {
	t.Parallel()
	d, err := sqlite.New(context.Background(), sqlite.Config{Path: filepath.Join(t.TempDir(), "snooze.db")})
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	plugs := map[string]plugins.Plugin{"user": &bulkRecordPlugin{name: "user"}}
	host := &bulkTestHost{driver: d, plugs: plugs}
	rt := &Router{DB: d, Host: host, Plugins: plugs}
	r := chi.NewRouter()
	rt.mountUser(r)
	rt.mountPeople(r)
	for _, p := range plugs {
		plugins.MountCRUD(r, host, p)
	}

	rec := peopleReq(t, r, http.MethodPut, "/api/v1/user/me/avatar", avatarBody(t, 16, 16), snoozetypes.DefaultTenant, "alice", "local")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), `"version"`)
	rec = peopleReq(t, r, http.MethodDelete, "/api/v1/user/me/avatar", nil, snoozetypes.DefaultTenant, "alice", "local")
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
}
