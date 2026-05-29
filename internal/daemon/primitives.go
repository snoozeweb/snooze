package daemon

import (
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/snoozeweb/snooze/internal/version"
)

// HandleVersion prints "<name> <version>" to w and returns true if argv
// requested the version subcommand. One-shot binaries that don't use Main
// (pacemaker, the googlechat stub) call this first, passing os.Stdout.
func HandleVersion(name string, args []string, w io.Writer) bool {
	if len(args) > 0 && args[0] == "version" {
		_, _ = fmt.Fprintln(w, name, version.String())
		return true
	}
	return false
}

// NewLogger builds the standard text slog.Logger (stderr, debug-gated level).
func NewLogger(debug bool) *slog.Logger { return newLogger(debug, os.Stderr) }

func newLogger(debug bool, w io.Writer) *slog.Logger {
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: level}))
}

// EnvOr returns the value of env var key, or def when unset/empty. Used by the
// few daemons that allow an env-var config-path override.
func EnvOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
