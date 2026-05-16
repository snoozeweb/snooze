// Package patlite implements the `patlite` Notifier plugin: it sets the
// light/buzzer state of a Patlite tower-light device (LR/LE series) over HTTP
// based on the severity of an alert.
//
// The Python plugin (src/snooze/plugins/core/patlite/) speaks the legacy
// Patlite TCP socket protocol on port 10000. Newer Patlite firmware exposes
// an HTTP control endpoint; this Go rewrite targets that surface as instructed
// by the Phase-5 spec. Concretely the plugin issues a GET against
// `http://<host>:<port><path>?color=<color>&state=<state>` (or `?clear=1`
// when the resolved state is "clear"). The exact query shape varies between
// firmwares — we keep the wire format simple and string-templated so that an
// operator can adjust it via the `path` config without code changes.
//
// # Config selection
//
// Notifier.Send receives a NotificationPayload whose Meta map may carry
// per-device overrides (host, port, path, timeout, tls_insecure,
// severity_map). When a key is absent the plugin's package defaults (or the
// values plugged in at construction time, e.g. by tests) apply. The
// severity-to-(color,state) lookup mirrors the Python action-form mental
// model but is keyed on `rec.Severity` directly: severity → entry, falling
// back to the `default` entry, finally to a hard-coded "clear" pulse so the
// device never gets left in a stale state on an unknown severity.
package patlite

import (
	"context"
	"crypto/tls"
	_ "embed"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

//go:embed metadata.yaml
var metaYAML []byte

const (
	// defaultPort is the standard HTTP port on a Patlite NH-series device.
	defaultPort = 80
	// defaultPath is the most common control endpoint name observed in the
	// LR/LE HTTP firmware.
	defaultPath = "/api/control"
	// defaultTimeout caps each HTTP call; Patlite devices respond in tens
	// of milliseconds on a healthy LAN.
	defaultTimeout = 5 * time.Second
	// severityKeyDefault is the fall-back severity key in severity_map.
	severityKeyDefault = "default"
)

// SeverityAction describes the patlite state to apply for a given severity:
// `Color` is one of red/amber/green/blue/white (or the literal "clear" /
// empty for "no light"); `State` is on/off/blink1/blink2 (and is ignored
// when Color == "clear", which sends `?clear=1`).
type SeverityAction struct {
	Color string `json:"color,omitempty" yaml:"color"`
	State string `json:"state,omitempty" yaml:"state"`
}

// IsClear reports whether the action is a "turn everything off" pulse.
func (a SeverityAction) IsClear() bool {
	c := strings.ToLower(strings.TrimSpace(a.Color))
	return c == "" || c == "clear" || c == "off"
}

// Config carries the per-device knobs. Tests construct it directly; in the
// running server it is decoded from the NotificationPayload.Meta map.
type Config struct {
	Host        string                    `json:"host" yaml:"host"`
	Port        int                       `json:"port,omitempty" yaml:"port"`
	Path        string                    `json:"path,omitempty" yaml:"path"`
	Timeout     time.Duration             `json:"timeout,omitempty" yaml:"timeout"`
	TLSInsecure bool                      `json:"tls_insecure,omitempty" yaml:"tls_insecure"`
	SeverityMap map[string]SeverityAction `json:"severity_map,omitempty" yaml:"severity_map"`
}

// defaultSeverityMap is the Python-fidelity-adjacent default mapping. The
// Python plugin lets the user wire everything by hand; we provide a sensible
// fallback so the plugin works out-of-the-box.
func defaultSeverityMap() map[string]SeverityAction {
	return map[string]SeverityAction{
		"critical":         {Color: "red", State: "on"},
		"error":            {Color: "red", State: "on"},
		"warning":          {Color: "amber", State: "on"},
		"info":             {Color: "green", State: "on"},
		severityKeyDefault: {Color: "clear"},
	}
}

// withDefaults returns a copy of c with zero fields replaced by package
// defaults. The receiver is not mutated.
func (c Config) withDefaults() Config {
	out := c
	if out.Port == 0 {
		out.Port = defaultPort
	}
	if out.Path == "" {
		out.Path = defaultPath
	}
	if out.Timeout == 0 {
		out.Timeout = defaultTimeout
	}
	if out.SeverityMap == nil {
		out.SeverityMap = defaultSeverityMap()
	}
	return out
}

// Plugin is the patlite Notifier.
type Plugin struct {
	meta   plugins.Metadata
	host   plugins.Host
	cfg    Config // package-level fallback config (typically zero)
	client *http.Client
}

// Name returns the registry key.
func (p *Plugin) Name() string { return "patlite" }

// Metadata returns the parsed metadata.yaml.
func (p *Plugin) Metadata() plugins.Metadata { return p.meta }

// PostInit captures the host reference. The plugin holds no DB state.
func (p *Plugin) PostInit(_ context.Context, host plugins.Host) error {
	p.host = host
	return nil
}

// Reload is a no-op: patlite is purely a notifier with no cached collection.
func (p *Plugin) Reload(_ context.Context) error { return nil }

// Send issues the HTTP control request to the Patlite device described by
// the payload meta (or by the plugin-level Config fallback).
func (p *Plugin) Send(ctx context.Context, rec snoozetypes.Record, payload plugins.NotificationPayload) error {
	cfg, err := p.resolveConfig(payload)
	if err != nil {
		return err
	}
	cfg = cfg.withDefaults()
	if cfg.Host == "" {
		return fmt.Errorf("patlite: host is required")
	}

	action := pickAction(cfg.SeverityMap, rec.Severity)
	reqURL, err := buildURL(cfg, action)
	if err != nil {
		return fmt.Errorf("patlite: build url: %w", err)
	}

	client := p.httpClient(cfg)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return fmt.Errorf("patlite: build request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("patlite: %s: %w", reqURL, err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode >= 400 {
		return fmt.Errorf("patlite: %s: unexpected status %d", reqURL, resp.StatusCode)
	}
	return nil
}

// resolveConfig merges the plugin-level fallback with the per-call overrides
// found in payload.Meta. The merge is shallow on top-level fields and on the
// severity_map entries.
func (p *Plugin) resolveConfig(payload plugins.NotificationPayload) (Config, error) {
	cfg := p.cfg
	if payload.Meta == nil {
		return cfg, nil
	}
	if v, ok := stringOf(payload.Meta["host"]); ok {
		cfg.Host = v
	}
	if v, ok := intOf(payload.Meta["port"]); ok {
		cfg.Port = v
	}
	if v, ok := stringOf(payload.Meta["path"]); ok {
		cfg.Path = v
	}
	if v, ok := durationOf(payload.Meta["timeout"]); ok {
		cfg.Timeout = v
	}
	if v, ok := boolOf(payload.Meta["tls_insecure"]); ok {
		cfg.TLSInsecure = v
	}
	if v, ok := payload.Meta["severity_map"]; ok {
		m, err := decodeSeverityMap(v)
		if err != nil {
			return cfg, err
		}
		if m != nil {
			cfg.SeverityMap = m
		}
	}
	return cfg, nil
}

// httpClient returns the HTTP client suited to cfg. Tests can override the
// transport by setting p.client directly.
func (p *Plugin) httpClient(cfg Config) *http.Client {
	if p.client != nil {
		return p.client
	}
	tr := &http.Transport{}
	if cfg.TLSInsecure {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // user-opted insecure mode
	}
	return &http.Client{
		Timeout:   cfg.Timeout,
		Transport: tr,
	}
}

// pickAction selects the SeverityAction for severity, falling back to the
// `default` entry, and finally to a hard-coded "clear" pulse.
func pickAction(m map[string]SeverityAction, severity string) SeverityAction {
	if a, ok := m[strings.ToLower(severity)]; ok {
		return a
	}
	if a, ok := m[severity]; ok {
		return a
	}
	if a, ok := m[severityKeyDefault]; ok {
		return a
	}
	return SeverityAction{Color: "clear"}
}

// buildURL composes the control-endpoint URL for the chosen action.
//
//   - When action.IsClear() the query becomes `?clear=1`.
//   - Otherwise it becomes `?color=<color>&state=<state>`; an empty state
//     defaults to "on".
//
// The path is treated verbatim: callers that need a different shape (e.g.
// the LE-A1 firmware's `/cgi-bin/lamp.cgi`) configure it via `path`.
func buildURL(cfg Config, action SeverityAction) (string, error) {
	scheme := "http"
	hostPort := cfg.Host
	if strings.HasPrefix(strings.ToLower(cfg.Host), "https://") {
		scheme = "https"
		hostPort = cfg.Host[len("https://"):]
	} else if strings.HasPrefix(strings.ToLower(cfg.Host), "http://") {
		hostPort = cfg.Host[len("http://"):]
	}
	// Allow callers to embed the port in the host already; otherwise append.
	if !strings.Contains(hostPort, ":") && cfg.Port > 0 {
		hostPort = hostPort + ":" + strconv.Itoa(cfg.Port)
	}

	path := cfg.Path
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	q := url.Values{}
	if action.IsClear() {
		q.Set("clear", "1")
	} else {
		q.Set("color", strings.ToLower(action.Color))
		state := strings.ToLower(strings.TrimSpace(action.State))
		if state == "" {
			state = "on"
		}
		q.Set("state", state)
	}

	u := &url.URL{
		Scheme:   scheme,
		Host:     hostPort,
		Path:     path,
		RawQuery: q.Encode(),
	}
	return u.String(), nil
}

// stringOf coerces a map[string]any value to a non-empty string.
func stringOf(v any) (string, bool) {
	s, ok := v.(string)
	if !ok || s == "" {
		return "", false
	}
	return s, true
}

// intOf coerces a JSON-like number (int, int64, float64) to int.
func intOf(v any) (int, bool) {
	switch x := v.(type) {
	case int:
		return x, true
	case int32:
		return int(x), true
	case int64:
		return int(x), true
	case float64:
		return int(x), true
	case string:
		if n, err := strconv.Atoi(x); err == nil {
			return n, true
		}
	}
	return 0, false
}

// boolOf coerces a JSON-like boolean to bool.
func boolOf(v any) (bool, bool) {
	switch x := v.(type) {
	case bool:
		return x, true
	case string:
		if b, err := strconv.ParseBool(x); err == nil {
			return b, true
		}
	}
	return false, false
}

// durationOf coerces a numeric or string value to a time.Duration. Numeric
// values are interpreted as seconds (to match the metadata.yaml `default_value`
// units); strings are parsed with time.ParseDuration.
func durationOf(v any) (time.Duration, bool) {
	switch x := v.(type) {
	case time.Duration:
		return x, true
	case int:
		return time.Duration(x) * time.Second, true
	case int64:
		return time.Duration(x) * time.Second, true
	case float64:
		return time.Duration(x * float64(time.Second)), true
	case string:
		if d, err := time.ParseDuration(x); err == nil {
			return d, true
		}
		if n, err := strconv.Atoi(x); err == nil {
			return time.Duration(n) * time.Second, true
		}
	}
	return 0, false
}

// decodeSeverityMap accepts either a typed map[string]SeverityAction or the
// JSON-decoded shape `map[string]any{...: map[string]any{"color":...,
// "state":...}}` that arrives from a wire payload.
func decodeSeverityMap(v any) (map[string]SeverityAction, error) {
	if v == nil {
		return nil, nil
	}
	if m, ok := v.(map[string]SeverityAction); ok {
		return m, nil
	}
	raw, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("patlite: severity_map must be an object, got %T", v)
	}
	out := make(map[string]SeverityAction, len(raw))
	for k, val := range raw {
		entry, err := decodeSeverityEntry(val)
		if err != nil {
			return nil, fmt.Errorf("patlite: severity_map[%q]: %w", k, err)
		}
		out[k] = entry
	}
	return out, nil
}

func decodeSeverityEntry(v any) (SeverityAction, error) {
	switch x := v.(type) {
	case SeverityAction:
		return x, nil
	case string:
		// Bare string is treated as the color, state defaults to "on".
		return SeverityAction{Color: x}, nil
	case map[string]any:
		var out SeverityAction
		if c, ok := stringOf(x["color"]); ok {
			out.Color = c
		}
		if s, ok := stringOf(x["state"]); ok {
			out.State = s
		}
		return out, nil
	default:
		return SeverityAction{}, fmt.Errorf("severity entry must be string or object, got %T", v)
	}
}

// factory is the plugins.Factory entry-point.
func factory(meta plugins.Metadata) (plugins.Plugin, error) {
	return &Plugin{meta: meta}, nil
}

func init() {
	plugins.Register("patlite", metaYAML, factory)
}
