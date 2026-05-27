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
	// ActionForm is a YAML-ordered map of (field name → descriptor). Order
	// matters: the React frontend renders fields in the order they appear
	// here, which is the order they were declared in metadata.yaml. A plain
	// map[string]any would lose that order on JSON marshal (Go sorts keys),
	// hence OrderedFields. See orderedfields.go.
	ActionForm OrderedFields `yaml:"action_form" json:"action_form,omitempty"`
	// SettingForm describes the typed catalogue of runtime settings the
	// "settings" plugin advertises. Same FormField shape as ActionForm, with
	// an optional per-field `group:` key the React frontend uses to render
	// settings grouped by section (general vs notification). Same ordering
	// concerns apply.
	SettingForm OrderedFields `yaml:"setting_form" json:"setting_form,omitempty"`
	Provides    []string      `yaml:"provides" json:"provides,omitempty"`
	// RouteDefaults is the plugin-level baseline for every route generated
	// or registered for this plugin. Individual Routes entries override
	// fields piecewise (see Metadata.ResolveRoute).
	//
	// Mirrors the Python `route_defaults:` block at the top of each
	// plugin's metadata.yaml.
	RouteDefaults Route `yaml:"route_defaults" json:"route_defaults,omitempty"`
	// Routes is keyed by route path (e.g. "/user_self") and carries
	// per-route overrides on top of RouteDefaults.
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

// Route is a single per-path override for the plugin's HTTP surface. A nil
// `Authentication` means "inherit from RouteDefaults (or the global default
// of true)"; an explicit `false` opts the route out of the Bearer-token
// requirement. AuthorizationPolicy works the same way: nil means "inherit",
// an empty struct {} means "no extra read/write grants (defaults only)".
type Route struct {
	ClassName           string               `yaml:"class_name" json:"class_name,omitempty"`
	CheckPermissions    bool                 `yaml:"check_permissions" json:"check_permissions,omitempty"`
	PrimaryKey          []string             `yaml:"primary_key" json:"primary_key,omitempty"`
	DuplicatePolicy     string               `yaml:"duplicate_policy" json:"duplicate_policy,omitempty"`
	Authentication      *bool                `yaml:"authentication" json:"authentication,omitempty"`
	AuthorizationPolicy *AuthorizationPolicy `yaml:"authorization_policy" json:"authorization_policy,omitempty"`
}

// AuthorizationPolicy mirrors the Python RouteArgs.authorization_policy.
// Each list names permissions that ALSO grant read / write access to the
// route on top of the default `ro_<plugin>` / `rw_<plugin>` / `ro_all` /
// `rw_all` grants. The special token `any` matches every authenticated
// caller (because the authorizer adds `any` to the user's effective
// permission set, mirroring 1.5.0's is_authorized).
type AuthorizationPolicy struct {
	Read  []string `yaml:"read" json:"read,omitempty"`
	Write []string `yaml:"write" json:"write,omitempty"`
}

// ResolveRoute returns the effective Route for a given path under the
// plugin. It starts from RouteDefaults and overlays the entry at
// m.Routes[path] when present. Unset (nil-pointer / zero-value) fields on
// the override inherit from the defaults.
//
// path is matched literally; callers wanting prefix-style resolution
// should compose their own lookup on top of this helper.
func (m Metadata) ResolveRoute(path string) Route {
	out := m.RouteDefaults
	override, ok := m.Routes[path]
	if !ok {
		return out
	}
	if override.ClassName != "" {
		out.ClassName = override.ClassName
	}
	if override.CheckPermissions {
		out.CheckPermissions = true
	}
	if len(override.PrimaryKey) > 0 {
		out.PrimaryKey = override.PrimaryKey
	}
	if override.DuplicatePolicy != "" {
		out.DuplicatePolicy = override.DuplicatePolicy
	}
	if override.Authentication != nil {
		v := *override.Authentication
		out.Authentication = &v
	}
	if override.AuthorizationPolicy != nil {
		// Copy-on-overlay so future edits don't bleed back into the
		// override map.
		cp := *override.AuthorizationPolicy
		out.AuthorizationPolicy = &cp
	}
	return out
}

// AuthenticationRequired returns true when the resolved route demands a
// valid Bearer token. The default is `true` — only an explicit
// `authentication: false` in metadata.yaml opts the route out. The path
// argument is the path-key under m.Routes; pass "" to consult only the
// defaults.
func (m Metadata) AuthenticationRequired(path string) bool {
	rt := m.ResolveRoute(path)
	if rt.Authentication == nil {
		return true
	}
	return *rt.Authentication
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
