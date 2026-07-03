package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/plugins"
)

// stubPlugin satisfies plugins.Plugin and (optionally) plugins.DataModel.
type stubPlugin struct {
	name     string
	meta     plugins.Metadata
	schema   any
	validate func(map[string]any) error
}

func (s *stubPlugin) Name() string                                     { return s.name }
func (s *stubPlugin) Metadata() plugins.Metadata                       { return s.meta }
func (s *stubPlugin) PostInit(_ context.Context, _ plugins.Host) error { return nil }
func (s *stubPlugin) Reload(_ context.Context) error                   { return nil }
func (s *stubPlugin) Schema() any                                      { return s.schema }
func (s *stubPlugin) Validate(m map[string]any) error {
	if s.validate == nil {
		return nil
	}
	return s.validate(m)
}

func TestSchemaRoute_ReturnsPluginSchema(t *testing.T) {
	rt := &Router{Plugins: map[string]plugins.Plugin{
		"record": &stubPlugin{
			name:   "record",
			schema: map[string]any{"type": "object"},
		},
	}}
	r := chi.NewRouter()
	rt.mountSchema(r)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/schema/record", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"type":"object"`)
}

func TestSchemaRoute_UnknownPlugin(t *testing.T) {
	rt := &Router{Plugins: map[string]plugins.Plugin{}}
	r := chi.NewRouter()
	rt.mountSchema(r)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/schema/missing", nil))
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestPermissions_UnionAcrossPlugins(t *testing.T) {
	rt := &Router{Plugins: map[string]plugins.Plugin{
		"record": &stubPlugin{name: "record"},
		"rule":   &stubPlugin{name: "rule", meta: plugins.Metadata{Provides: []string{"notifier", "receiver"}}},
	}}
	r := chi.NewRouter()
	rt.mountPermissions(r)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/permissions", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	var got struct {
		Data []string `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Contains(t, got.Data, "rw_all")
	require.Contains(t, got.Data, "ro_all")
	require.Contains(t, got.Data, "rw_record")
	require.Contains(t, got.Data, "ro_record")
	require.Contains(t, got.Data, "rw_rule")
	require.Contains(t, got.Data, "ro_rule")
	// `provides` entries are capability tags (notifier / receiver), not
	// assignable permissions: nothing in the authorizer honours them, so they
	// must NOT leak into the catalogue. Only provides entries also wired into
	// an authorization_policy (e.g. can_comment) are surfaced — via that policy,
	// not via provides. See TestPermissions_AuthorizationPolicyIncluded.
	require.NotContains(t, got.Data, "notifier")
	require.NotContains(t, got.Data, "receiver")
}

func TestPermissions_AuthorizationPolicyIncluded(t *testing.T) {
	rt := &Router{Plugins: map[string]plugins.Plugin{
		"record": &stubPlugin{name: "record", meta: plugins.Metadata{
			RouteDefaults: plugins.Route{
				AuthorizationPolicy: &plugins.AuthorizationPolicy{
					Read:  []string{"any"},
					Write: []string{"can_comment"},
				},
			},
		}},
	}}
	r := chi.NewRouter()
	rt.mountPermissions(r)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/permissions", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	var got struct {
		Data []string `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Contains(t, got.Data, "can_comment")
	require.NotContains(t, got.Data, "any")
}

func TestPermissions_RouteOverrideIncluded(t *testing.T) {
	rt := &Router{Plugins: map[string]plugins.Plugin{
		"record": &stubPlugin{name: "record", meta: plugins.Metadata{
			Routes: map[string]plugins.Route{
				"/x": {
					AuthorizationPolicy: &plugins.AuthorizationPolicy{
						Write: []string{"can_escalate"},
					},
				},
			},
		}},
	}}
	r := chi.NewRouter()
	rt.mountPermissions(r)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/permissions", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	var got struct {
		Data []string `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Contains(t, got.Data, "can_escalate")
}
