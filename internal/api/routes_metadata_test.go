package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/plugins"
)

// TestMetadataRoute_ListReturnsRegisteredPlugins asserts the bulk endpoint
// returns every registered plugin's full metadata in the `data` array, with
// the action_form sub-object preserved verbatim (so the React frontend can
// render typed action forms from it).
func TestMetadataRoute_ListReturnsRegisteredPlugins(t *testing.T) {
	mailMeta := plugins.Metadata{
		Name:        "mail",
		DisplayName: "Send email",
		Icon:        "envelope",
		ActionForm: map[string]any{
			"host": map[string]any{
				"display_name":  "Host",
				"component":     "String",
				"default_value": "localhost",
			},
		},
	}
	rt := &Router{Plugins: map[string]plugins.Plugin{
		"mail":   &stubPlugin{name: "mail", meta: mailMeta},
		"record": &stubPlugin{name: "record", meta: plugins.Metadata{Name: "record", DisplayName: "Records"}},
	}}
	r := chi.NewRouter()
	rt.mountMetadata(r)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/metadata", nil))
	require.Equal(t, http.StatusOK, rec.Code)

	var got struct {
		Data []map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Len(t, got.Data, 2)

	// Locate the mail entry — order is unspecified.
	var mail map[string]any
	for _, m := range got.Data {
		if m["name"] == "mail" {
			mail = m
			break
		}
	}
	require.NotNil(t, mail, "mail plugin missing from response")
	require.Equal(t, "Send email", mail["display_name"])
	require.Equal(t, "envelope", mail["icon"])
	form, ok := mail["action_form"].(map[string]any)
	require.True(t, ok, "action_form must be an object")
	host, ok := form["host"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "Host", host["display_name"])
	require.Equal(t, "String", host["component"])
	require.Equal(t, "localhost", host["default_value"])
}

// TestMetadataRoute_OneReturnsSinglePlugin asserts the per-plugin endpoint
// returns just that plugin and a 404 when the name is unknown.
func TestMetadataRoute_OneReturnsSinglePlugin(t *testing.T) {
	rt := &Router{Plugins: map[string]plugins.Plugin{
		"mail": &stubPlugin{
			name: "mail",
			meta: plugins.Metadata{Name: "mail", DisplayName: "Send email", Icon: "envelope"},
		},
	}}
	r := chi.NewRouter()
	rt.mountMetadata(r)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/metadata/mail", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	var got struct {
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Equal(t, "mail", got.Data["name"])
	require.Equal(t, "Send email", got.Data["display_name"])
	require.Equal(t, "envelope", got.Data["icon"])

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/metadata/missing", nil))
	require.Equal(t, http.StatusNotFound, rec.Code)
}

// TestMetadataRoute_ListInjectsPluginName asserts every entry in the bulk
// response carries `plugin_name` equal to the registry key, independent of
// whatever the plugin's YAML `name:` field happens to be. The frontend keys
// off `plugin_name` to render typed action forms (the YAML `name:` is a
// display label in most action plugins).
func TestMetadataRoute_ListInjectsPluginName(t *testing.T) {
	rt := &Router{Plugins: map[string]plugins.Plugin{
		"mail": &stubPlugin{
			name: "mail",
			// YAML `name:` is a human label here — mimics the real metadata.yaml.
			meta: plugins.Metadata{Name: "Send email", DisplayName: "Send email"},
		},
		"script": &stubPlugin{
			name: "script",
			meta: plugins.Metadata{Name: "Run a script", DisplayName: "Run a script"},
		},
		"patlite": &stubPlugin{
			name: "patlite",
			meta: plugins.Metadata{Name: "patlite", DisplayName: "Patlite"},
		},
	}}
	r := chi.NewRouter()
	rt.mountMetadata(r)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/metadata", nil))
	require.Equal(t, http.StatusOK, rec.Code)

	var got struct {
		Data []map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Len(t, got.Data, 3)

	byKey := map[string]map[string]any{}
	for _, m := range got.Data {
		pn, ok := m["plugin_name"].(string)
		require.True(t, ok, "every entry must carry a plugin_name")
		byKey[pn] = m
	}
	require.Contains(t, byKey, "mail")
	require.Contains(t, byKey, "script")
	require.Contains(t, byKey, "patlite")
	// The mismatched-name regression: yaml `name:` is "Send email", but the
	// registry key (and therefore plugin_name) is "mail".
	require.Equal(t, "Send email", byKey["mail"]["name"])
	require.Equal(t, "mail", byKey["mail"]["plugin_name"])
	require.Equal(t, "Run a script", byKey["script"]["name"])
	require.Equal(t, "script", byKey["script"]["plugin_name"])
}

// TestMetadataRoute_OneInjectsPluginName asserts the single-plugin endpoint
// also stamps `plugin_name` with the registry key (so the React frontend
// can reliably find a typed form regardless of the plugin's YAML name).
func TestMetadataRoute_OneInjectsPluginName(t *testing.T) {
	rt := &Router{Plugins: map[string]plugins.Plugin{
		"mail": &stubPlugin{
			name: "mail",
			meta: plugins.Metadata{Name: "Send email", DisplayName: "Send email"},
		},
	}}
	r := chi.NewRouter()
	rt.mountMetadata(r)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/metadata/mail", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	var got struct {
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Equal(t, "Send email", got.Data["name"])
	require.Equal(t, "mail", got.Data["plugin_name"])
}

// TestMetadataRoute_SettingsCatalogue locks in the contract between the
// settings plugin's metadata.yaml and the React Settings page: the parsed
// `setting_form` must surface every documented runtime-setting key with its
// FormField shape (including the `group:` selector the frontend uses to
// render the picker grouped by section).
func TestMetadataRoute_SettingsCatalogue(t *testing.T) {
	// Resolve the settings plugin's metadata.yaml relative to this test file
	// so the test stays portable across `go test ./...` invocations regardless
	// of cwd. The settings plugin lives at internal/pluginimpl/settings.
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller failed")
	yamlPath := filepath.Join(
		filepath.Dir(thisFile), "..", "pluginimpl", "settings", "metadata.yaml",
	)
	data, err := os.ReadFile(yamlPath)
	require.NoError(t, err)
	meta, err := plugins.ParseMetadata(data)
	require.NoError(t, err)
	require.Equal(t, "Settings", meta.Name)
	require.NotNil(t, meta.SettingForm, "settings plugin must advertise a setting_form")

	rt := &Router{Plugins: map[string]plugins.Plugin{
		"settings": &stubPlugin{name: "settings", meta: meta},
	}}
	r := chi.NewRouter()
	rt.mountMetadata(r)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/metadata/settings", nil))
	require.Equal(t, http.StatusOK, rec.Code)

	var got struct {
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	form, ok := got.Data["setting_form"].(map[string]any)
	require.True(t, ok, "setting_form must be an object")

	// Both documented sections must surface at least one entry each, so we
	// can be confident the metadata.yaml is structurally complete.
	for _, key := range []string{
		"default_auth_backend",
		"local_users_enabled",
		"metrics_enabled",
		"anonymous_enabled",
		"ok_severities",
		"notification_freq",
		"notification_retry",
	} {
		require.Contains(t, form, key, "missing setting %q", key)
	}

	// The `group:` field on each entry must thread through unchanged — the
	// React frontend uses it to render <optgroup>s in the picker.
	authField, ok := form["default_auth_backend"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "Selector", authField["component"])
	require.Equal(t, "general", authField["group"])
	require.Equal(t, "local", authField["default_value"])

	freqField, ok := form["notification_freq"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "String", freqField["component"])
	require.Equal(t, "notification", freqField["group"])

	// LDAP and Housekeeping entries land in setting_form with their dotted
	// names. We don't enumerate every field — the YAML is the source of
	// truth — but assert a representative sample so a missing prefix would
	// fail the build instead of degrading silently in the UI.
	for _, key := range []string{
		"ldap.enabled", "ldap.host", "ldap.port", "ldap.bind_password",
		"housekeeping.trigger_on_startup", "housekeeping.cleanup_snooze",
		"housekeeping.cleanup_notification", "housekeeping.cleanup_audit",
	} {
		require.Contains(t, form, key, "missing runtime-editable setting %q", key)
	}
	// Password component routes to a masked field on the frontend; the
	// settings plugin advertises it for bind_password specifically.
	bindPwd, ok := form["ldap.bind_password"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "Password", bindPwd["component"])
	require.Equal(t, "ldap", bindPwd["group"])
	// Housekeeping intervals are typed as String so the wire format stays
	// a Go duration literal.
	snoozeInt, ok := form["housekeeping.cleanup_snooze"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "String", snoozeInt["component"])
	require.Equal(t, "housekeeping", snoozeInt["group"])
}
