// Package k8sevents implements the snooze-k8s-events daemon: it watches the
// Kubernetes core/v1 Event API over plain HTTP (no k8s.io/client-go) and
// forwards interesting events to snooze-server as snoozetypes.Record alerts.
//
// The daemon talks to the apiserver one of two ways:
//
//   - in-cluster: it reads the projected ServiceAccount token from
//     /var/run/secrets/kubernetes.io/serviceaccount/token, trusts the CA at
//     .../ca.crt, and derives the apiserver URL from KUBERNETES_SERVICE_HOST /
//     KUBERNETES_SERVICE_PORT. This is the default when `apiserver` is empty.
//   - explicit: the operator supplies `apiserver`, a bearer `token` (or
//     `token_file`), and either `ca_cert` or `insecure: true`.
//
// It then issues GET {apiserver}/api/v1/events?watch=true&resourceVersion=<rv>
// and streams the newline-delimited {type,object} watch envelopes, mapping each
// Warning (and optionally Normal) Event to a Record posted via pkg/snoozeclient.
package k8sevents

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Standard in-cluster service-account mount points. They are variables (not
// consts) so tests can point them at a temp dir.
var (
	inClusterTokenFile = "/var/run/secrets/kubernetes.io/serviceaccount/token" //nolint:gosec // G101: filesystem path to the ServiceAccount token file, not an embedded credential
	inClusterCAFile    = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
)

// defaultReasonSeverity maps well-known Event.reason values to a Snooze
// severity. Reasons not listed fall back to the type-based default
// (Warning→warning, Normal→info). Operators can override or extend this via
// Config.Reasons.
var defaultReasonSeverity = map[string]string{
	// Memory pressure / OOM is the canonical "page someone now" event.
	"OOMKilling":   "critical",
	"Killing":      "critical",
	"SystemOOM":    "critical",
	"NodeNotReady": "critical",

	// Hard scheduling / mount / image-pull failures keep workloads down but
	// are usually recoverable, so they land at error rather than critical.
	"FailedScheduling":       "error",
	"FailedMount":            "error",
	"FailedAttachVolume":     "error",
	"BackOff":                "error",
	"CrashLoopBackOff":       "error",
	"Failed":                 "error",
	"FailedCreatePodSandBox": "error",
	"Unhealthy":              "error",
	"FailedKillPod":          "error",
}

// Config is the YAML schema for /etc/snooze/k8s-events.yaml. It bundles the
// standard Snooze-client block with the watcher's own knobs.
type Config struct {
	// --- Snooze client block (shared shape across all daemons) ---

	// Server is the Snooze base URL ("https://snooze.example.com"). Required.
	Server string `yaml:"server"`

	// Username / Password authenticate against the Snooze v1 /login endpoint.
	// Required unless Token is supplied.
	Username string `yaml:"username"`
	Password string `yaml:"password"`

	// Method selects the Snooze auth backend. Defaults to "local".
	Method string `yaml:"method"`

	// Token short-circuits the Snooze login flow and is used as the bearer
	// token directly. (This is the SNOOZE token, not the Kubernetes one.)
	Token string `yaml:"token"`

	// Insecure disables TLS verification for the Snooze HTTPS client.
	Insecure bool `yaml:"insecure"`

	// --- Kubernetes apiserver block ---

	// APIServer is the kube-apiserver URL ("https://10.0.0.1:6443"). When
	// empty the daemon auto-detects in-cluster configuration from the
	// KUBERNETES_SERVICE_HOST / KUBERNETES_SERVICE_PORT env vars and the
	// projected ServiceAccount token + CA.
	APIServer string `yaml:"apiserver"`

	// K8sToken is the Kubernetes bearer token. When empty the daemon reads
	// K8sTokenFile (defaulting to the in-cluster mount).
	K8sToken string `yaml:"token_k8s"`

	// K8sTokenFile is a path to a file containing the Kubernetes bearer token.
	// Defaults to the in-cluster mount when running in-cluster.
	K8sTokenFile string `yaml:"token_file"`

	// CACert is a path to a PEM file the daemon trusts for the apiserver TLS
	// connection. Defaults to the in-cluster CA when running in-cluster.
	CACert string `yaml:"ca_cert"`

	// K8sInsecure disables TLS verification for the apiserver connection.
	// Mutually exclusive with a meaningful CACert.
	K8sInsecure bool `yaml:"insecure_skip_verify"`

	// Namespace scopes the watch to a single namespace. Empty ("") watches all
	// namespaces via the cluster-wide /api/v1/events endpoint.
	Namespace string `yaml:"namespace"`

	// IncludeNormal, when true, forwards Normal events as well as Warning
	// events. Off by default — Normal events are high-volume and rarely
	// actionable.
	IncludeNormal bool `yaml:"include_normal"`

	// EventTypes overrides the set of Event.type values to forward. When set it
	// takes precedence over IncludeNormal. Values are matched case-sensitively
	// against the Kubernetes type ("Warning", "Normal").
	EventTypes []string `yaml:"event_types"`

	// Reasons overrides/extends the reason→severity map. Keys are Event.reason
	// values, values are Snooze severities. Merged on top of the built-in
	// defaultReasonSeverity.
	Reasons map[string]string `yaml:"reasons"`

	// ResyncInterval bounds how long a single watch connection stays open
	// before the daemon proactively reconnects (the apiserver also times
	// watches out server-side). Defaults to 30m. Accepts Go duration syntax.
	ResyncInterval time.Duration `yaml:"resync_interval"`

	// DedupWindow suppresses repeated events for the same
	// involvedObject+reason seen within the window. Defaults to 1m. Set to 0
	// to disable de-duplication.
	DedupWindow time.Duration `yaml:"dedup_window"`

	// RequestTimeout caps a single (non-watch) HTTP request — the Snooze POST
	// and the initial list. Defaults to 30s. The watch request itself uses no
	// client timeout (it is a long-lived stream bounded by ResyncInterval).
	RequestTimeout time.Duration `yaml:"request_timeout"`

	// Debug enables debug-level logging.
	Debug bool `yaml:"debug"`
}

// LoadConfig reads a YAML file at path and returns a fully-defaulted Config.
func LoadConfig(path string) (Config, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // path is operator-supplied
	if err != nil {
		return Config{}, fmt.Errorf("k8sevents: read config %q: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return Config{}, fmt.Errorf("k8sevents: parse config %q: %w", path, err)
	}
	return cfg.WithDefaults()
}

// WithDefaults fills zero-value fields with documented defaults, auto-detects
// in-cluster configuration when APIServer is empty, and validates the required
// fields. It returns a copy so callers keep the original for diagnostics.
func (c Config) WithDefaults() (Config, error) {
	if strings.TrimSpace(c.Server) == "" {
		return c, fmt.Errorf("k8sevents: server is required")
	}
	if c.Method == "" {
		c.Method = "local"
	}

	// Auto-detect in-cluster when no explicit apiserver was given.
	if strings.TrimSpace(c.APIServer) == "" {
		host := os.Getenv("KUBERNETES_SERVICE_HOST")
		port := os.Getenv("KUBERNETES_SERVICE_PORT")
		if host == "" {
			return c, fmt.Errorf("k8sevents: apiserver is empty and KUBERNETES_SERVICE_HOST is not set (not running in-cluster?)")
		}
		if port == "" {
			port = "443"
		}
		c.APIServer = fmt.Sprintf("https://%s", joinHostPort(host, port))
		// Fill the in-cluster token/CA defaults only when the operator did not
		// already supply explicit material.
		if c.K8sToken == "" && c.K8sTokenFile == "" {
			c.K8sTokenFile = inClusterTokenFile
		}
		if c.CACert == "" && !c.K8sInsecure {
			c.CACert = inClusterCAFile
		}
	}
	c.APIServer = strings.TrimRight(c.APIServer, "/")

	// An explicit apiserver still needs a way to authenticate and to trust TLS.
	if c.K8sToken == "" && c.K8sTokenFile == "" {
		return c, fmt.Errorf("k8sevents: a Kubernetes token_k8s or token_file is required")
	}
	if c.CACert == "" && !c.K8sInsecure {
		return c, fmt.Errorf("k8sevents: set ca_cert or insecure_skip_verify for the apiserver connection")
	}

	if c.ResyncInterval <= 0 {
		c.ResyncInterval = 30 * time.Minute
	}
	if c.DedupWindow < 0 {
		c.DedupWindow = 0
	} else if c.DedupWindow == 0 {
		c.DedupWindow = time.Minute
	}
	if c.RequestTimeout <= 0 {
		c.RequestTimeout = 30 * time.Second
	}

	// Merge the operator's reason overrides on top of the built-in defaults so
	// a partial override doesn't wipe the rest of the map.
	merged := make(map[string]string, len(defaultReasonSeverity)+len(c.Reasons))
	for k, v := range defaultReasonSeverity {
		merged[k] = v
	}
	for k, v := range c.Reasons {
		merged[k] = strings.ToLower(strings.TrimSpace(v))
	}
	c.Reasons = merged

	return c, nil
}

// joinHostPort wraps an IPv6 literal in brackets before joining host:port.
// net.JoinHostPort would do this too but pulling in net here for one call is
// avoidable — keep the helper local and stdlib-free at the type level.
func joinHostPort(host, port string) string {
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		return "[" + host + "]:" + port
	}
	return host + ":" + port
}

// wantsType reports whether an Event.type should be forwarded given the
// configuration. EventTypes (when set) wins; otherwise Warning is always
// forwarded and Normal only when IncludeNormal is true.
func (c Config) wantsType(eventType string) bool {
	if len(c.EventTypes) > 0 {
		for _, t := range c.EventTypes {
			if t == eventType {
				return true
			}
		}
		return false
	}
	switch eventType {
	case "Warning":
		return true
	case "Normal":
		return c.IncludeNormal
	default:
		// Unknown/empty type: forward only when we'd forward Normal, since the
		// apiserver occasionally emits events with no type set.
		return c.IncludeNormal
	}
}
