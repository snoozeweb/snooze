package plugins

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// Metadata mirrors the Python MetadataConfig: it captures the static
// description of a plugin parsed from its metadata.yaml. Routes are merged
// with route_defaults at plugin instantiation time.
type Metadata struct {
	Name            string         `yaml:"name"`
	DisplayName     string         `yaml:"desc"`
	Icon            string         `yaml:"icon"`
	DefaultSorting  string         `yaml:"default_sorting"`
	DefaultOrdering string         `yaml:"default_ordering"`
	AutoReload      bool           `yaml:"auto_reload"`
	Widgets         map[string]any `yaml:"widgets"`
	ActionForm      map[string]any `yaml:"action_form"`
	Provides        []string       `yaml:"provides"`
	// Routes is keyed by route path (e.g. "/webhook/alertmanager/v4") and
	// carries per-route overrides over the plugin's default Route.
	Routes       map[string]Route `yaml:"routes"`
	Options      map[string]any   `yaml:"options"`
	SearchFields []string         `yaml:"search_fields"`
	Audit        bool             `yaml:"audit"`
	// ForceOrder accepts either an int (the Go-port convention) or a string
	// (the legacy Python convention where sentinels like "tree_order"
	// carried the priority hint). Non-numeric strings collapse to 0 — the
	// plugin then behaves like an unordered one. See the custom UnmarshalYAML
	// on Metadata for the parsing details.
	ForceOrder int  `yaml:"force_order"`
	Tree       bool `yaml:"tree"`
}

// Route is a single per-path override for the plugin's HTTP surface.
type Route struct {
	ClassName        string   `yaml:"class_name"`
	CheckPermissions bool     `yaml:"check_permissions"`
	PrimaryKey       []string `yaml:"primary_key"`
	DuplicatePolicy  string   `yaml:"duplicate_policy"`
	Authentication   bool     `yaml:"authentication"`
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
