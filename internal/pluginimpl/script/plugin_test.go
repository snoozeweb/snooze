package script

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	"github.com/snoozeweb/snooze/internal/config"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/internal/telemetry"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// stubHost is a minimal plugins.Host for the unit tests in this package. We
// don't need the database driver — script.Plugin doesn't touch the DB.
type stubHost struct{}

func (stubHost) DB() db.Driver                { return nil }
func (stubHost) Bus() plugins.Bus             { return nil }
func (stubHost) Logger() *slog.Logger         { return slog.Default() }
func (stubHost) Tracer() trace.Tracer         { return otel.Tracer("script-test") }
func (stubHost) Metrics() *telemetry.Registry { return telemetry.NewRegistry(nil) }
func (stubHost) Config() *config.Config       { return config.Default() }
func (stubHost) Plugin(string) plugins.Plugin { return nil }

func newPlugin(t *testing.T) *Plugin {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("script plugin tests require POSIX /bin/sh, /bin/cat and /bin/echo")
	}
	p := &Plugin{meta: plugins.Metadata{Name: "script"}}
	require.NoError(t, p.PostInit(context.Background(), stubHost{}))
	return p
}

func sampleRecord() snoozetypes.Record {
	return snoozetypes.Record{
		UID:      "rec-1",
		Host:     "db01.example.com",
		Source:   "syslog",
		Severity: "warning",
		Message:  "disk almost full",
	}
}

func TestRegistration(t *testing.T) {
	require.True(t, slices.Contains(plugins.Registered(), "script"),
		"script plugin should be registered via init()")
}

func TestMetadata(t *testing.T) {
	p := &Plugin{meta: plugins.Metadata{Name: "script"}}
	require.Equal(t, "script", p.Name())
	require.NoError(t, p.Reload(context.Background()))
}

func TestSend(t *testing.T) {
	t.Run("simple echo", func(t *testing.T) {
		p := newPlugin(t)
		payload := plugins.NotificationPayload{
			Meta: map[string]any{
				"command": []any{"/bin/echo", "hello", "{{ .Record.Host }}"},
				"timeout": 5,
			},
		}
		err := p.Send(context.Background(), sampleRecord(), payload)
		require.NoError(t, err)
	})

	t.Run("non-zero exit code surfaces ExecError", func(t *testing.T) {
		p := newPlugin(t)
		payload := plugins.NotificationPayload{
			Meta: map[string]any{
				"command": []any{"/bin/sh", "-c", "echo boom >&2; exit 7"},
				"timeout": 5,
			},
		}
		err := p.Send(context.Background(), sampleRecord(), payload)
		require.Error(t, err)
		var execErr *ExecError
		require.True(t, errors.As(err, &execErr), "expected *ExecError, got %T", err)
		require.Equal(t, "exit", execErr.Reason)
		require.Equal(t, 7, execErr.ExitCode)
		require.Contains(t, execErr.Output, "boom")
	})

	t.Run("timeout kills sleeping child", func(t *testing.T) {
		p := newPlugin(t)
		payload := plugins.NotificationPayload{
			Meta: map[string]any{
				"command": []any{"/bin/sh", "-c", "sleep 5"},
				"timeout": 0.2, // 200ms — well under the 5s sleep
			},
		}
		start := time.Now()
		err := p.Send(context.Background(), sampleRecord(), payload)
		elapsed := time.Since(start)
		require.Error(t, err)
		var execErr *ExecError
		require.True(t, errors.As(err, &execErr))
		require.Equal(t, "timeout", execErr.Reason)
		require.Less(t, elapsed, 4*time.Second,
			"expected the process to be killed near the 200ms deadline, took %s", elapsed)
	})

	t.Run("stdout cap truncates large output", func(t *testing.T) {
		p := newPlugin(t)
		// Print 4096 'A' bytes; cap to 100.
		payload := plugins.NotificationPayload{
			Meta: map[string]any{
				"command":    []any{"/bin/sh", "-c", "printf 'A%.0s' $(seq 1 4096); exit 1"},
				"timeout":    5,
				"max_output": 100,
			},
		}
		err := p.Send(context.Background(), sampleRecord(), payload)
		require.Error(t, err)
		var execErr *ExecError
		require.True(t, errors.As(err, &execErr))
		require.Equal(t, "exit", execErr.Reason)
		require.True(t, strings.HasSuffix(execErr.Output, truncatedMarker),
			"expected truncated marker, got tail: %q",
			lastN(execErr.Output, 32))
		// Body without the marker should equal max_output.
		body := strings.TrimSuffix(execErr.Output, truncatedMarker)
		require.Equal(t, 100, len(body))
	})

	t.Run("stdin templating feeds the child", func(t *testing.T) {
		p := newPlugin(t)
		// `cat` echoes whatever it receives on stdin, but we redirect to a
		// temp file so we can assert the exact bytes the child saw.
		tmp, err := os.CreateTemp(t.TempDir(), "stdin-*")
		require.NoError(t, err)
		require.NoError(t, tmp.Close())

		payload := plugins.NotificationPayload{
			Meta: map[string]any{
				"command": []any{"/bin/sh", "-c", "cat > " + tmp.Name()},
				"stdin":   `host={{ .Record.Host }}; severity={{ .Record.Severity }}`,
				"timeout": 5,
			},
		}
		require.NoError(t, p.Send(context.Background(), sampleRecord(), payload))

		got, err := os.ReadFile(tmp.Name())
		require.NoError(t, err)
		require.Equal(t, "host=db01.example.com; severity=warning", string(got))
	})

	t.Run("missing command rejected", func(t *testing.T) {
		p := newPlugin(t)
		err := p.Send(context.Background(), sampleRecord(),
			plugins.NotificationPayload{Meta: map[string]any{}})
		require.Error(t, err)
		require.Contains(t, err.Error(), "command")
	})

	t.Run("env prepends without dropping inherited PATH", func(t *testing.T) {
		p := newPlugin(t)
		// Print SCRIPT_TEST_KEY then PATH — both must be populated.
		payload := plugins.NotificationPayload{
			Meta: map[string]any{
				"command": []any{"/bin/sh", "-c",
					`printf 'key=%s path_set=%s' "$SCRIPT_TEST_KEY" "$( [ -n "$PATH" ] && echo yes || echo no )"`},
				"env": map[string]any{
					"SCRIPT_TEST_KEY": "{{ .Record.Severity }}",
				},
				"timeout":    5,
				"max_output": 1024,
			},
		}
		// On a zero-exit run we don't get the output back through ExecError,
		// so route it through a temp file instead.
		tmp, err := os.CreateTemp(t.TempDir(), "env-*")
		require.NoError(t, err)
		require.NoError(t, tmp.Close())
		payload.Meta["command"] = []any{"/bin/sh", "-c",
			`printf 'key=%s path_set=%s' "$SCRIPT_TEST_KEY" "$( [ -n "$PATH" ] && echo yes || echo no )" > ` + tmp.Name()}

		require.NoError(t, p.Send(context.Background(), sampleRecord(), payload))
		got, err := os.ReadFile(tmp.Name())
		require.NoError(t, err)
		require.Equal(t, "key=warning path_set=yes", string(got))
	})
}

func lastN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

func TestParseConfigDefaults(t *testing.T) {
	cfg, err := parseConfig(map[string]any{
		"command": []any{"/bin/true"},
	})
	require.NoError(t, err)
	require.Equal(t, defaultTimeout, cfg.timeout)
	require.Equal(t, defaultMaxOutput, cfg.maxOutput)
	require.Equal(t, []string{"/bin/true"}, cfg.command)
}

func TestParseConfigInvalid(t *testing.T) {
	cases := []struct {
		name string
		meta map[string]any
	}{
		{"nil meta", nil},
		{"no command", map[string]any{}},
		{"empty command", map[string]any{"command": []any{}}},
		{"bad timeout type", map[string]any{"command": []any{"a"}, "timeout": "soon"}},
		{"negative timeout", map[string]any{"command": []any{"a"}, "timeout": -1}},
		{"negative max_output", map[string]any{"command": []any{"a"}, "max_output": 0}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseConfig(tc.meta)
			require.Error(t, err)
		})
	}
}

func TestCapWriter(t *testing.T) {
	t.Run("under cap leaves output untouched", func(t *testing.T) {
		w := newCapWriter(100)
		n, err := w.Write([]byte("short"))
		require.NoError(t, err)
		require.Equal(t, 5, n)
		require.Equal(t, "short", w.String())
	})

	t.Run("over cap truncates and marks", func(t *testing.T) {
		w := newCapWriter(4)
		n, err := w.Write([]byte("AAAAAAA"))
		require.NoError(t, err)
		require.Equal(t, 7, n, "Write must report the full input length to keep os/exec happy")
		require.Equal(t, "AAAA"+truncatedMarker, w.String())
	})

	t.Run("multiple writes accumulate then truncate", func(t *testing.T) {
		w := newCapWriter(5)
		_, _ = w.Write([]byte("abc"))
		_, _ = w.Write([]byte("defghi"))
		require.Equal(t, "abcde"+truncatedMarker, w.String())
	})
}
