// Package pacemaker implements the snooze-pacemaker one-shot fence helper.
//
// Pacemaker (and the stonith-ng fence-agent contract more generally) invokes a
// fence helper with a single "action" argument — either passive (metadata,
// monitor, list, status) or destructive (on, off, reboot, validate-all).
// Parameters are passed either positionally on the command line or, more
// commonly, via environment variables read from stdin (key=value lines).
//
// snooze-pacemaker is intentionally minimal: it does NOT perform any fencing
// itself. Its only job is to forward a single fence-event record to the
// Snooze v1 alert API so the cluster operator gets a paper trail every time
// a node is shot.  Passive actions exit 0 without touching the network so
// `crm_mon` / `pcs stonith status` style probes stay cheap.
package pacemaker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/snoozeweb/snooze/pkg/snoozeclient"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// DefaultConfigPath is the canonical on-disk location for the helper's config.
const DefaultConfigPath = "/etc/snooze/pacemaker.yaml"

// metadataXML is the (very small) fence-agent metadata document. Pacemaker's
// stonith-ng calls `<agent> metadata` at registration time to discover the
// parameter list. Returning a minimal-but-valid document keeps `pcs stonith
// describe snooze-pacemaker` happy.
const metadataXML = `<?xml version="1.0" ?>
<resource-agent name="snooze-pacemaker" shortdesc="Snooze fence-event reporter">
  <longdesc>Posts a fence-event record to the Snooze v1 alert API. This helper does not perform any fencing on its own; it is meant to be used alongside a real fence agent (e.g. fence_ipmilan) for observability.</longdesc>
  <parameters>
    <parameter name="nodename"><shortdesc>Name of the node being fenced.</shortdesc><content type="string"/></parameter>
    <parameter name="port"><shortdesc>Alias of nodename, accepted for fence-agent compatibility.</shortdesc><content type="string"/></parameter>
    <parameter name="reason"><shortdesc>Optional human-readable fence reason.</shortdesc><content type="string"/></parameter>
  </parameters>
  <actions>
    <action name="on"/>
    <action name="off"/>
    <action name="reboot"/>
    <action name="status"/>
    <action name="monitor"/>
    <action name="list"/>
    <action name="metadata"/>
    <action name="validate-all"/>
  </actions>
</resource-agent>
`

// fenceActions is the set of actions that translate into a real fence-event
// record. Everything else is treated as a passive probe and returns 0 without
// posting.
var fenceActions = map[string]struct{}{
	"on":     {},
	"off":    {},
	"reboot": {},
}

// passiveActions is the set of actions that exit 0 with no network activity.
// We list them explicitly (rather than "anything not in fenceActions") so an
// unknown action returns a non-zero exit code instead of silently passing.
var passiveActions = map[string]struct{}{
	"status":       {},
	"monitor":      {},
	"list":         {},
	"validate-all": {},
}

// Config is the YAML schema for /etc/snooze/pacemaker.yaml. The shape mirrors
// the other component configs so operators only need to learn it once.
//
// All fields can be overridden by environment variables; see Runner.Run for
// the precedence rules.
type Config struct {
	// Server is the Snooze base URL. Required (env: SNOOZE_SERVER).
	Server string `yaml:"server"`
	// Username and Password authenticate against the v1 /login endpoint.
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	// Method selects the auth backend; "" defaults to "local".
	Method string `yaml:"method"`
	// Token short-circuits Login when set.
	Token string `yaml:"token"`
	// Insecure disables TLS verification.
	Insecure bool `yaml:"insecure"`
	// RequestTimeout caps each HTTP request. Defaults to 10s — fence
	// helpers run on hot paths, we don't want them to hang the cluster.
	RequestTimeout time.Duration `yaml:"request_timeout"`
}

// LoadConfig reads the YAML config at path. A missing file is NOT an error —
// the caller can still supply everything via the environment. Any other I/O
// or parse error is surfaced verbatim.
func LoadConfig(path string) (Config, error) {
	if path == "" {
		return Config{}, nil
	}
	raw, err := os.ReadFile(path) //nolint:gosec // path is operator-supplied
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Config{}, nil
		}
		return Config{}, fmt.Errorf("pacemaker: read config %q: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return Config{}, fmt.Errorf("pacemaker: parse config %q: %w", path, err)
	}
	return cfg, nil
}

// Runner is the one-shot pacemaker fence helper. Build one with NewRunner and
// call Run exactly once.
type Runner struct {
	cfg    Config
	env    map[string]string
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
	logger *slog.Logger
}

// Options bundles the knobs accepted by NewRunner. Zero values are filled in
// with the documented defaults.
type Options struct {
	// Config is the parsed YAML (may be the zero value when running entirely
	// from the environment).
	Config Config
	// Env, when non-nil, is the environment map seen by the runner. Defaults
	// to the live process environment.
	Env map[string]string
	// Stdin is the byte stream from which "key=value\n" parameter lines are
	// read. Defaults to os.Stdin. Reading is bounded (1 MiB) so a hostile
	// caller can't OOM us.
	Stdin io.Reader
	// Stdout and Stderr default to os.Stdout / os.Stderr. metadata writes
	// XML to Stdout; everything else only writes to Stderr on error.
	Stdout io.Writer
	Stderr io.Writer
	// Logger defaults to slog.Default().
	Logger *slog.Logger
}

// NewRunner constructs a Runner with sensible defaults.
func NewRunner(o Options) *Runner {
	r := &Runner{
		cfg:    o.Config,
		env:    o.Env,
		stdin:  o.Stdin,
		stdout: o.Stdout,
		stderr: o.Stderr,
		logger: o.Logger,
	}
	if r.env == nil {
		r.env = envSliceToMap(os.Environ())
	}
	if r.stdin == nil {
		r.stdin = os.Stdin
	}
	if r.stdout == nil {
		r.stdout = os.Stdout
	}
	if r.stderr == nil {
		r.stderr = os.Stderr
	}
	if r.logger == nil {
		r.logger = slog.Default()
	}
	return r
}

// Run executes the fence-helper once and returns the (POSIX) exit code plus,
// for diagnostics, the underlying error.  It NEVER calls os.Exit itself so
// main() stays testable.
//
// Argument and environment protocol (matches the fence-agent spec):
//
//   - args[0] = action ("metadata", "monitor", "list", "status", "on", "off",
//     "reboot", "validate-all"). When empty, defaults to the value of $action
//     or $ACTION, mirroring stonith-ng's stdin-driven invocation.
//   - $nodename / $port (or args[1]) names the host being fenced.
//   - $reason (optional) overrides the default fence message.
//   - Additional "key=value" lines may be supplied on stdin; recognised keys
//     are folded into the env map before the action runs.
//
// Exit codes:
//
//	0 — action handled successfully (fence record posted or passive no-op)
//	1 — configuration error (missing server, etc.)
//	2 — unknown action
//	3 — Snooze API error
func (r *Runner) Run(ctx context.Context, args []string) (int, error) {
	// 1. Merge stdin key=value pairs into the env map. Existing env vars win,
	//    matching the fence-agent convention (stdin is a fallback when the
	//    parent process can't or won't set environment variables).
	if err := r.mergeStdinParams(); err != nil {
		_, _ = fmt.Fprintln(r.stderr, "snooze-pacemaker:", err)
		return 1, err
	}

	action := resolveAction(args, r.env)
	if action == "" {
		err := errors.New("snooze-pacemaker: no action supplied (pass as arg or set $action)")
		_, _ = fmt.Fprintln(r.stderr, err)
		return 2, err
	}

	if action == "metadata" {
		_, _ = io.WriteString(r.stdout, metadataXML)
		return 0, nil
	}

	if _, ok := passiveActions[action]; ok {
		// Passive actions don't talk to Snooze — they just acknowledge the
		// helper is alive. `list` historically prints a node list; we don't
		// have one to advertise, so emit an empty stdout (the parent uses
		// the exit code to decide success).
		return 0, nil
	}

	if _, ok := fenceActions[action]; !ok {
		err := fmt.Errorf("snooze-pacemaker: unknown action %q", action)
		_, _ = fmt.Fprintln(r.stderr, err)
		return 2, err
	}

	// 2. Build the snooze record. Host can come from positional arg, the
	//    "nodename" / "port" env vars, or (last resort) the hostname.
	host := resolveHost(args, r.env)
	if host == "" {
		err := errors.New("snooze-pacemaker: no node name supplied (pass as arg or set $nodename)")
		_, _ = fmt.Fprintln(r.stderr, err)
		return 1, err
	}
	reason := strings.TrimSpace(r.env["reason"])
	if reason == "" {
		reason = strings.TrimSpace(r.env["REASON"])
	}
	rec := buildRecord(action, host, reason)

	// 3. Resolve credentials. Env overrides config.
	opts, err := r.buildClientOptions()
	if err != nil {
		_, _ = fmt.Fprintln(r.stderr, "snooze-pacemaker:", err)
		return 1, err
	}

	client, err := snoozeclient.New(opts)
	if err != nil {
		_, _ = fmt.Fprintln(r.stderr, "snooze-pacemaker: build client:", err)
		return 1, err
	}

	// 4. Login (best-effort when credentials are present and no token cache
	//    seeded the client) then post.
	if opts.Token == "" && opts.Username != "" {
		if err := client.Login(ctx); err != nil {
			r.logger.Warn("pacemaker: initial login failed, retrying via PostAlert", slog.Any("err", err))
		}
	}
	if _, err := client.PostAlert(ctx, rec); err != nil {
		_, _ = fmt.Fprintln(r.stderr, "snooze-pacemaker: post alert:", err)
		return 3, fmt.Errorf("pacemaker: post alert: %w", err)
	}

	r.logger.Info("pacemaker: fence event recorded",
		slog.String("action", action),
		slog.String("host", host),
	)
	return 0, nil
}

// buildRecord assembles the canonical fence-event Record.
func buildRecord(action, host, reason string) snoozetypes.Record {
	msg := reason
	if msg == "" {
		msg = fmt.Sprintf("fence %s requested for node %s", action, host)
	}
	return snoozetypes.Record{
		Host:      host,
		Source:    "pacemaker",
		Process:   "fence",
		Severity:  "critical",
		Message:   msg,
		Timestamp: time.Now().UTC(),
		Tags:      []string{"fence", "cluster"},
		Raw: map[string]any{
			"action": action,
			"host":   host,
			"reason": reason,
		},
	}
}

// resolveAction picks the action from args or env, in that order. The fence
// spec allows lowercase only, but we accept any case for ergonomics.
func resolveAction(args []string, env map[string]string) string {
	if len(args) > 0 && args[0] != "" {
		return strings.ToLower(strings.TrimSpace(args[0]))
	}
	for _, key := range []string{"action", "ACTION"} {
		if v := strings.TrimSpace(env[key]); v != "" {
			return strings.ToLower(v)
		}
	}
	return ""
}

// resolveHost picks the fenced-node name from args[1], the fence-agent's
// "nodename" / "port" env vars, or finally $HOSTNAME (best-effort).
func resolveHost(args []string, env map[string]string) string {
	if len(args) > 1 && strings.TrimSpace(args[1]) != "" {
		return strings.TrimSpace(args[1])
	}
	for _, key := range []string{"nodename", "NODENAME", "port", "PORT"} {
		if v := strings.TrimSpace(env[key]); v != "" {
			return v
		}
	}
	return ""
}

// buildClientOptions merges the YAML config with environment overrides.
// Environment wins because operators wire credentials through systemd unit
// files or stonith resource parameters rather than rewriting YAML for every
// host.
func (r *Runner) buildClientOptions() (snoozeclient.Options, error) {
	opts := snoozeclient.Options{
		BaseURL:        pickString(r.env["SNOOZE_SERVER"], r.cfg.Server),
		Username:       pickString(r.env["SNOOZE_USERNAME"], r.cfg.Username),
		Password:       pickString(r.env["SNOOZE_PASSWORD"], r.cfg.Password),
		Method:         pickString(r.env["SNOOZE_METHOD"], r.cfg.Method),
		Token:          pickString(r.env["SNOOZE_TOKEN"], r.cfg.Token),
		TokenCacheFile: r.env["SNOOZE_TOKEN_CACHE_FILE"],
		Logger:         r.logger.With(slog.String("component", "snoozeclient")),
	}
	opts.Insecure = r.cfg.Insecure || parseBoolEnv(r.env["SNOOZE_INSECURE"])
	if r.cfg.RequestTimeout > 0 {
		opts.Timeout = r.cfg.RequestTimeout
	} else {
		opts.Timeout = 10 * time.Second
	}
	if opts.BaseURL == "" {
		return opts, errors.New("snooze-pacemaker: SNOOZE_SERVER (or config.server) is required")
	}
	return opts, nil
}

// mergeStdinParams reads "key=value" lines from stdin (capped at 1 MiB) and
// folds them into the env map without overwriting existing entries. This
// mirrors how stonith-ng passes parameters to fence agents.
func (r *Runner) mergeStdinParams() error {
	if r.stdin == nil {
		return nil
	}
	// Stop reading after 1 MiB — the spec is bounded, anything larger is a
	// bug or an attack.
	const maxStdin = 1 << 20
	raw, err := io.ReadAll(io.LimitReader(r.stdin, maxStdin))
	if err != nil {
		return fmt.Errorf("read stdin: %w", err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		val := strings.TrimSpace(line[eq+1:])
		if _, present := r.env[key]; !present {
			r.env[key] = val
		}
	}
	return nil
}

// envSliceToMap converts os.Environ()'s "KEY=VAL" form into a map.
func envSliceToMap(env []string) map[string]string {
	out := make(map[string]string, len(env))
	for _, kv := range env {
		eq := strings.IndexByte(kv, '=')
		if eq <= 0 {
			continue
		}
		out[kv[:eq]] = kv[eq+1:]
	}
	return out
}

// pickString returns the first non-empty string in the list.
func pickString(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// parseBoolEnv returns true for any of {"1","true","yes","on"} (case insensitive).
func parseBoolEnv(v string) bool {
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "" {
		return false
	}
	if b, err := strconv.ParseBool(v); err == nil {
		return b
	}
	switch v {
	case "yes", "on":
		return true
	}
	return false
}
