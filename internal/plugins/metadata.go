package plugins

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// Metadata mirrors the Python MetadataConfig: it captures the static
// description of a plugin parsed from its metadata.yaml. Routes are merged
// with route_defaults at plugin instantiation time.
//
// The JSON tags exist so /api/v1/metadata serialises with the same snake_case
// keys the YAML uses — the React frontend reads action_form / display_name /
// etc. directly without a remap step.
type Metadata struct {
	// PluginName is the registry key (Plugin.Name()); injected by the
	// metadata HTTP handler before serialisation so the frontend has a
	// stable, machine-readable identifier independent of the YAML `name:`
	// field (which is a human display label in most plugins, e.g. "Send
	// email" / "Run a script"). The `yaml:"-"` tag keeps it out of the
	// YAML round-trip.
	PluginName      string         `yaml:"-" json:"plugin_name"`
	Name            string         `yaml:"name" json:"name"`
	DisplayName     string         `yaml:"desc" json:"display_name"`
	Icon            string         `yaml:"icon" json:"icon,omitempty"`
	DefaultSorting  string         `yaml:"default_sorting" json:"default_sorting,omitempty"`
	DefaultOrdering string         `yaml:"default_ordering" json:"default_ordering,omitempty"`
	AutoReload      bool           `yaml:"auto_reload" json:"auto_reload,omitempty"`
	Widgets         map[string]any `yaml:"widgets" json:"widgets,omitempty"`
	ActionForm      map[string]any `yaml:"action_form" json:"action_form,omitempty"`
	// SettingForm describes the typed catalogue of runtime settings the
	// "settings" plugin advertises. Same FormField shape as ActionForm, with
	// an optional per-field `group:` key the React frontend uses to render
	// settings grouped by section (general vs notification).
	SettingForm map[string]any `yaml:"setting_form" json:"setting_form,omitempty"`
	Provides    []string       `yaml:"provides" json:"provides,omitempty"`
	// Routes is keyed by route path (e.g. "/webhook/alertmanager/v4") and
	// carries per-route overrides over the plugin's default Route.
	Routes       map[string]Route `yaml:"routes" json:"routes,omitempty"`
	Options      map[string]any   `yaml:"options" json:"options,omitempty"`
	SearchFields []string         `yaml:"search_fields" json:"search_fields,omitempty"`
	Audit        bool             `yaml:"audit" json:"audit"`
	// ForceOrder accepts either an int (the Go-port convention) or a string
	// (the legacy Python convention where sentinels like "tree_order"
	// carried the priority hint). Non-numeric strings collapse to 0 — the
	// plugin then behaves like an unordered one. See the custom UnmarshalYAML
	// on Metadata for the parsing details.
	ForceOrder int  `yaml:"force_order" json:"force_order,omitempty"`
	Tree       bool `yaml:"tree" json:"tree,omitempty"`
}

// Route is a single per-path override for the plugin's HTTP surface.
type Route struct {
	ClassName        string   `yaml:"class_name" json:"class_name,omitempty"`
	CheckPermissions bool     `yaml:"check_permissions" json:"check_permissions,omitempty"`
	PrimaryKey       []string `yaml:"primary_key" json:"primary_key,omitempty"`
	DuplicatePolicy  string   `yaml:"duplicate_policy" json:"duplicate_policy,omitempty"`
	Authentication   bool     `yaml:"authentication" json:"authentication,omitempty"`
}

// ParseMetadata decodes a metadata.yaml byte slice into a Metadata value.
// Empty input yields a zero-value Metadata and no error.
func ParseMetadata(data []byte) (Metadata, error) {
	var m Metadata
	if len(data) == 0 {
		return m, nil
	}
	if err := yaml.Unmarshal(data, &m); err != nil {
		return Metadata{}, fmt.Errorf("plugins: parse metadata: %w", err)
	}
	return m, nil
}

// UnmarshalYAML defaults Audit to true when the key is absent. The Python
// convention is "audit unless declared otherwise" — declaring `audit: false`
// in metadata.yaml is an explicit opt-out for noisy collections (audit,
// comment, kv). Without this, every plugin would have to repeat
// `audit: true`, since Go's zero value would otherwise hide the default.
func (m *Metadata) UnmarshalYAML(value *yaml.Node) error {
	// alias breaks the recursive method call on Metadata.
	type alias Metadata
	aux := alias{Audit: true}
	if err := value.Decode(&aux); err != nil {
		return err
	}
	*m = Metadata(aux)
	return nil
}
