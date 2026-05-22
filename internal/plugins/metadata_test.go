package plugins

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseMetadata_Empty(t *testing.T) {
	t.Parallel()
	m, err := ParseMetadata(nil)
	require.NoError(t, err)
	require.Equal(t, Metadata{}, m)

	m, err = ParseMetadata([]byte(""))
	require.NoError(t, err)
	require.Equal(t, Metadata{}, m)
}

func TestParseMetadata_RuleSample(t *testing.T) {
	// Matches src/snooze/plugins/core/rule/metadata.yaml literally.
	t.Parallel()
	yamlText := []byte(`---
name: 'Rule'
desc: 'Rule desc'
auto_reload: true
force_order: 0
tree: true
`)
	m, err := ParseMetadata(yamlText)
	require.NoError(t, err)
	require.Equal(t, "Rule", m.Name)
	require.Equal(t, "Rule desc", m.DisplayName)
	require.True(t, m.AutoReload)
	require.True(t, m.Tree)
	require.Equal(t, 0, m.ForceOrder)
}

func TestParseMetadata_AlertManagerStyle(t *testing.T) {
	// Exercise the Routes / route_defaults block from the alertmanager plugin.
	t.Parallel()
	yamlText := []byte(`---
name: 'AlertManager'
desc: 'AlertManager'
routes:
  /webhook/alertmanager/v4:
    class_name: 'AlertManagerV4Route'
    check_permissions: false
    authentication: false
    primary_key: ['uid']
    duplicate_policy: 'update'
`)
	m, err := ParseMetadata(yamlText)
	require.NoError(t, err)
	require.Equal(t, "AlertManager", m.Name)
	require.Contains(t, m.Routes, "/webhook/alertmanager/v4")
	r := m.Routes["/webhook/alertmanager/v4"]
	require.Equal(t, "AlertManagerV4Route", r.ClassName)
	require.False(t, r.CheckPermissions)
	// Authentication is *bool now; "authentication: false" should decode
	// to a non-nil pointer with the explicit false value.
	require.NotNil(t, r.Authentication)
	require.False(t, *r.Authentication)
	require.Equal(t, []string{"uid"}, r.PrimaryKey)
	require.Equal(t, "update", r.DuplicatePolicy)
}

func TestParseMetadata_RecordSearchFields(t *testing.T) {
	// Mirrors src/snooze/plugins/core/record/metadata.yaml.
	t.Parallel()
	yamlText := []byte(`---
name: 'Record'
desc: 'Record desc'
search_fields:
  - timestamp
  - host
  - process
  - severity
  - source
  - message
`)
	m, err := ParseMetadata(yamlText)
	require.NoError(t, err)
	require.Equal(t, []string{"timestamp", "host", "process", "severity", "source", "message"}, m.SearchFields)
}

func TestParseMetadata_FullSurface(t *testing.T) {
	// Exercises every Metadata field at least once.
	t.Parallel()
	yamlText := []byte(`---
name: 'kitchen'
desc: 'Kitchen sink'
icon: 'sink'
default_sorting: 'created_at'
default_ordering: 'desc'
auto_reload: true
audit: true
force_order: 5
tree: false
search_fields: [a, b]
provides: [read, write]
widgets:
  main: {kind: list}
options:
  batch:
    default: true
    timer: 30
action_form:
  to:
    component: String
routes:
  /custom:
    class_name: Custom
    check_permissions: true
    authentication: true
    primary_key: [uid, host]
    duplicate_policy: replace
`)
	m, err := ParseMetadata(yamlText)
	require.NoError(t, err)
	require.Equal(t, "kitchen", m.Name)
	require.Equal(t, "Kitchen sink", m.DisplayName)
	require.Equal(t, "sink", m.Icon)
	require.Equal(t, "created_at", m.DefaultSorting)
	require.Equal(t, "desc", m.DefaultOrdering)
	require.True(t, m.AutoReload)
	require.True(t, m.Audit)
	require.Equal(t, 5, m.ForceOrder)
	require.False(t, m.Tree)
	require.Equal(t, []string{"a", "b"}, m.SearchFields)
	require.Equal(t, []string{"read", "write"}, m.Provides)
	require.NotEmpty(t, m.Widgets)
	require.NotEmpty(t, m.Options)
	require.NotEmpty(t, m.ActionForm)
	require.Contains(t, m.Routes, "/custom")
	require.Equal(t, "Custom", m.Routes["/custom"].ClassName)
	require.True(t, m.Routes["/custom"].CheckPermissions)
	require.NotNil(t, m.Routes["/custom"].Authentication)
	require.True(t, *m.Routes["/custom"].Authentication)
	require.Equal(t, []string{"uid", "host"}, m.Routes["/custom"].PrimaryKey)
	require.Equal(t, "replace", m.Routes["/custom"].DuplicatePolicy)
}

func TestParseMetadata_Invalid(t *testing.T) {
	t.Parallel()
	_, err := ParseMetadata([]byte("not: valid: yaml: ::"))
	require.Error(t, err)
}
