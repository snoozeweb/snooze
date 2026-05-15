// Package script implements the "script" Notifier plugin: it executes a
// configured local command and feeds the alert record to it on stdin or as
// templated argv slots.
//
// Hardening differences from the Python port:
//
//   - The command is exec'd directly via exec.CommandContext — never through
//     `sh -c`. Users who want shell semantics must spell the command list as
//     ["sh", "-c", "..."] explicitly.
//   - A wall-clock timeout (default 10s) is enforced via the context the
//     process is launched under.
//   - Combined stdout+stderr is capped (default 64 KiB) and truncated with a
//     trailing "... [truncated]" marker.
package script

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"text/template"
	"time"

	"github.com/japannext/snooze/internal/plugins"
	"github.com/japannext/snooze/pkg/snoozetypes"
)

//go:embed metadata.yaml
var metaYAML []byte

// Defaults for the soft-limit knobs. Both are overridable per-invocation
// through the NotificationPayload.Meta map.
const (
	defaultTimeout   = 10 * time.Second
	defaultMaxOutput = 64 * 1024 // 64 KiB
	truncatedMarker  = "... [truncated]"
)

func init() {
	plugins.Register("script", metaYAML, factory)
}

func factory(meta plugins.Metadata) (plugins.Plugin, error) {
	return &Plugin{meta: meta}, nil
}

// Plugin is the script notifier.
type Plugin struct {
	meta plugins.Metadata
	host plugins.Host
}

// Name returns the registry key. We hardcode "script" (matching mail /
// webhook / patlite) so the key stays machine-readable even though our
// metadata.yaml's `name:` field is a human display label ("Run a script").
// The frontend uses this registry key to look up the typed action_form.
func (p *Plugin) Name() string { return "script" }

// Metadata returns the parsed metadata.yaml descriptor.
func (p *Plugin) Metadata() plugins.Metadata { return p.meta }

// PostInit captures the host for subsequent calls.
func (p *Plugin) PostInit(_ context.Context, host plugins.Host) error {
	p.host = host
	return nil
}

// Reload is a no-op: script configuration is supplied per invocation via the
// notification action document, not loaded from a collection here.
func (p *Plugin) Reload(_ context.Context) error { return nil }

// Send executes the configured command. The configuration is carried in
// payload.Meta with the keys documented in metadata.yaml:
//
//   - command    []any | []string  (required, first element is program)
//   - cwd        string            (optional, templated)
//   - env        map[string]any    (optional, values templated)
//   - stdin      string            (optional, templated)
//   - timeout    number (seconds)  (optional, default 10)
//   - max_output number (bytes)    (optional, default 65536)
//
// Send returns an error when the configuration is invalid, the process exits
// non-zero, or the timeout expires. Captured output (capped) is included in
// the returned error's Output field via *ExecError.
func (p *Plugin) Send(ctx context.Context, rec snoozetypes.Record, payload plugins.NotificationPayload) error {
	cfg, err := parseConfig(payload.Meta)
	if err != nil {
		return err
	}

	tplData := templateData(rec, payload)

	rawArgs, err := renderStrings(cfg.command, tplData)
	if err != nil {
		return fmt.Errorf("script: render command: %w", err)
	}
	if len(rawArgs) == 0 || strings.TrimSpace(rawArgs[0]) == "" {
		return errors.New("script: command must not be empty")
	}

	stdinData, err := renderString(cfg.stdin, tplData)
	if err != nil {
		return fmt.Errorf("script: render stdin: %w", err)
	}

	renderedCWD, err := renderString(cfg.cwd, tplData)
	if err != nil {
		return fmt.Errorf("script: render cwd: %w", err)
	}

	renderedEnv := make(map[string]string, len(cfg.env))
	for k, v := range cfg.env {
		val, err := renderString(v, tplData)
		if err != nil {
			return fmt.Errorf("script: render env[%s]: %w", k, err)
		}
		renderedEnv[k] = val
	}

	// Bound the child process with both the caller context and a wall clock.
	execCtx, cancel := context.WithTimeout(ctx, cfg.timeout)
	defer cancel()

	cmd := exec.CommandContext(execCtx, rawArgs[0], rawArgs[1:]...) //nolint:gosec // argv is intentionally user-controlled
	// WaitDelay caps the time Run() spends waiting for inherited stdout/
	// stderr pipes after the process exits — without it, a grandchild that
	// inherited our pipes (e.g. `sh -c "sleep 60"`) would keep Run() blocked
	// even after exec.CommandContext kills the direct child.
	cmd.WaitDelay = 250 * time.Millisecond

	if renderedCWD != "" {
		cmd.Dir = renderedCWD
	}

	if len(renderedEnv) > 0 {
		// Prepend so user-set values win over inherited ones.
		base := os.Environ()
		merged := make([]string, 0, len(renderedEnv)+len(base))
		for k, v := range renderedEnv {
			merged = append(merged, k+"="+v)
		}
		merged = append(merged, base...)
		cmd.Env = merged
	}

	if stdinData != "" {
		cmd.Stdin = strings.NewReader(stdinData)
	}

	combined := newCapWriter(cfg.maxOutput)
	cmd.Stdout = combined
	cmd.Stderr = combined

	runErr := cmd.Run()
	output := combined.String()

	// Distinguish timeout from other errors so callers can react.
	if execCtx.Err() == context.DeadlineExceeded {
		return &ExecError{
			Reason:  "timeout",
			Timeout: cfg.timeout,
			Output:  output,
			Err:     execCtx.Err(),
		}
	}
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			return &ExecError{
				Reason:   "exit",
				ExitCode: exitErr.ExitCode(),
				Output:   output,
				Err:      runErr,
			}
		}
		return &ExecError{
			Reason: "exec",
			Output: output,
			Err:    runErr,
		}
	}

	if lg := p.logger(); lg != nil {
		lg.Debug("script: command succeeded",
			"argv0", rawArgs[0],
			"argc", len(rawArgs),
			"output_bytes", len(output),
		)
	}
	return nil
}

func (p *Plugin) logger() *slog.Logger {
	if p.host == nil {
		return nil
	}
	return p.host.Logger()
}

// ExecError carries the captured (capped) output alongside the cause. It is
// returned for non-zero exits, timeouts, and exec-time failures (e.g.
// command-not-found).
type ExecError struct {
	// Reason classifies the failure: "exit" | "timeout" | "exec".
	Reason string
	// ExitCode is the child exit code when Reason == "exit".
	ExitCode int
	// Timeout is the configured deadline when Reason == "timeout".
	Timeout time.Duration
	// Output is the truncated combined stdout+stderr.
	Output string
	// Err is the underlying error from os/exec.
	Err error
}

func (e *ExecError) Error() string {
	switch e.Reason {
	case "exit":
		return fmt.Sprintf("script: exit %d: %v", e.ExitCode, e.Err)
	case "timeout":
		return fmt.Sprintf("script: timed out after %s", e.Timeout)
	default:
		return fmt.Sprintf("script: exec: %v", e.Err)
	}
}

func (e *ExecError) Unwrap() error { return e.Err }

// scriptConfig is the parsed view of payload.Meta.
type scriptConfig struct {
	command   []string
	cwd       string
	env       map[string]string
	stdin     string
	timeout   time.Duration
	maxOutput int
}

func parseConfig(meta map[string]any) (scriptConfig, error) {
	cfg := scriptConfig{
		timeout:   defaultTimeout,
		maxOutput: defaultMaxOutput,
	}

	if meta == nil {
		return cfg, errors.New("script: payload.Meta is required (must carry the action subcontent)")
	}

	cmdAny, ok := meta["command"]
	if !ok {
		return cfg, errors.New("script: command is required")
	}
	cmd, err := toStringSlice(cmdAny)
	if err != nil {
		return cfg, fmt.Errorf("script: command: %w", err)
	}
	if len(cmd) == 0 {
		return cfg, errors.New("script: command must not be empty")
	}
	cfg.command = cmd

	if v, ok := meta["cwd"]; ok {
		s, err := toString(v)
		if err != nil {
			return cfg, fmt.Errorf("script: cwd: %w", err)
		}
		cfg.cwd = s
	}

	if v, ok := meta["env"]; ok {
		env, err := toStringMap(v)
		if err != nil {
			return cfg, fmt.Errorf("script: env: %w", err)
		}
		cfg.env = env
	}

	if v, ok := meta["stdin"]; ok {
		s, err := toString(v)
		if err != nil {
			return cfg, fmt.Errorf("script: stdin: %w", err)
		}
		cfg.stdin = s
	}

	if v, ok := meta["timeout"]; ok {
		secs, err := toFloat(v)
		if err != nil {
			return cfg, fmt.Errorf("script: timeout: %w", err)
		}
		if secs <= 0 {
			return cfg, errors.New("script: timeout must be positive")
		}
		cfg.timeout = time.Duration(secs * float64(time.Second))
	}

	if v, ok := meta["max_output"]; ok {
		n, err := toFloat(v)
		if err != nil {
			return cfg, fmt.Errorf("script: max_output: %w", err)
		}
		if n <= 0 {
			return cfg, errors.New("script: max_output must be positive")
		}
		cfg.maxOutput = int(n)
	}

	return cfg, nil
}

// templateData is the value rendered by every text/template in the config.
// Exposing a struct keeps the per-template dot stable and lets users write
// e.g. {{ .Record.Host }}, {{ .Payload.Subject }}, {{ .RecordJSON }}.
type tmplCtx struct {
	Record     snoozetypes.Record
	Payload    plugins.NotificationPayload
	RecordJSON string
}

func templateData(rec snoozetypes.Record, payload plugins.NotificationPayload) tmplCtx {
	data := tmplCtx{Record: rec, Payload: payload}
	if b, err := json.Marshal(rec); err == nil {
		data.RecordJSON = string(b)
	}
	return data
}

func renderStrings(in []string, data any) ([]string, error) {
	out := make([]string, 0, len(in))
	for i, raw := range in {
		s, err := renderString(raw, data)
		if err != nil {
			return nil, fmt.Errorf("arg %d: %w", i, err)
		}
		out = append(out, s)
	}
	return out, nil
}

func renderString(raw string, data any) (string, error) {
	if raw == "" {
		return "", nil
	}
	// Fast-path: avoid template parsing when the input has no actions.
	if !strings.Contains(raw, "{{") {
		return raw, nil
	}
	tpl, err := template.New("script").Option("missingkey=zero").Parse(raw)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// toStringSlice accepts []string, []any (of strings/numbers/bools coerced),
// or rejects the value with an explanatory error.
func toStringSlice(v any) ([]string, error) {
	switch x := v.(type) {
	case []string:
		out := make([]string, len(x))
		copy(out, x)
		return out, nil
	case []any:
		out := make([]string, 0, len(x))
		for i, item := range x {
			s, err := toString(item)
			if err != nil {
				return nil, fmt.Errorf("element %d: %w", i, err)
			}
			out = append(out, s)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("expected []string, got %T", v)
	}
}

func toStringMap(v any) (map[string]string, error) {
	switch x := v.(type) {
	case map[string]string:
		out := make(map[string]string, len(x))
		for k, val := range x {
			out[k] = val
		}
		return out, nil
	case map[string]any:
		out := make(map[string]string, len(x))
		for k, val := range x {
			s, err := toString(val)
			if err != nil {
				return nil, fmt.Errorf("key %q: %w", k, err)
			}
			out[k] = s
		}
		return out, nil
	default:
		return nil, fmt.Errorf("expected map[string]string, got %T", v)
	}
}

func toString(v any) (string, error) {
	switch x := v.(type) {
	case string:
		return x, nil
	case fmt.Stringer:
		return x.String(), nil
	case bool:
		if x {
			return "true", nil
		}
		return "false", nil
	case int:
		return fmt.Sprintf("%d", x), nil
	case int64:
		return fmt.Sprintf("%d", x), nil
	case float64:
		return fmt.Sprintf("%v", x), nil
	default:
		return "", fmt.Errorf("expected string, got %T", v)
	}
}

func toFloat(v any) (float64, error) {
	switch x := v.(type) {
	case float64:
		return x, nil
	case float32:
		return float64(x), nil
	case int:
		return float64(x), nil
	case int32:
		return float64(x), nil
	case int64:
		return float64(x), nil
	case string:
		var f float64
		_, err := fmt.Sscanf(x, "%f", &f)
		if err != nil {
			return 0, fmt.Errorf("not a number: %q", x)
		}
		return f, nil
	default:
		return 0, fmt.Errorf("expected number, got %T", v)
	}
}

// capWriter is an io.Writer that captures up to N bytes into an internal
// buffer and silently discards the rest, marking the result as truncated
// when (and only when) the cap is exceeded.
//
// We use it as both cmd.Stdout and cmd.Stderr — os/exec serialises writes
// across the two when they share an io.Writer (it calls the underlying
// writer with a mutex), so a sync.Mutex inside this type would be redundant.
// We keep the writes intentionally simple and rely on os/exec's behaviour.
type capWriter struct {
	max       int
	buf       bytes.Buffer
	truncated bool
}

func newCapWriter(max int) *capWriter {
	return &capWriter{max: max}
}

func (c *capWriter) Write(p []byte) (int, error) {
	if c.truncated {
		// Pretend we accepted the whole write so exec.Cmd doesn't surface a
		// short-write error back through cmd.Run().
		return len(p), nil
	}
	remaining := c.max - c.buf.Len()
	if remaining <= 0 {
		c.truncated = true
		return len(p), nil
	}
	if len(p) <= remaining {
		return c.buf.Write(p)
	}
	if _, err := c.buf.Write(p[:remaining]); err != nil {
		return 0, err
	}
	c.truncated = true
	return len(p), nil
}

func (c *capWriter) String() string {
	if !c.truncated {
		return c.buf.String()
	}
	return c.buf.String() + truncatedMarker
}

// Ensure interfaces are satisfied at compile time.
var (
	_ plugins.Plugin   = (*Plugin)(nil)
	_ plugins.Notifier = (*Plugin)(nil)
	_ io.Writer        = (*capWriter)(nil)
	_ error            = (*ExecError)(nil)
)
