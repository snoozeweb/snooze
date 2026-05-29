// Package daemon provides the shared entry-point scaffolding for the auxiliary
// snooze-* binaries: the version subcommand, -c/-debug flags, slog setup, the
// automaxprocs side effect, a signal-driven run loop, and standardized exit
// codes. snooze-server and the snooze CLI do not use it.
package daemon

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/snoozeweb/snooze/internal/version"

	// Side-effect import: at init, automaxprocs sets GOMAXPROCS from the cgroup
	// CPU quota. Absorbed from the former internal/runtime package so every
	// binary that uses the harness gets it automatically.
	_ "go.uber.org/automaxprocs"
)

// Runnable is a long-running unit the harness drives until shutdown.
type Runnable interface {
	Run(ctx context.Context) error
}

// Config declares how one auxiliary binary boots.
type Config struct {
	// Name is the binary name, used in version output and error prefixes.
	Name string
	// DefaultConfig is the -c default (the systemd-installed path).
	DefaultConfig string
	// Build loads the binary's own config from cfgPath and constructs the
	// Runnable. The closure absorbs each component's LoadConfig+New shape.
	Build func(cfgPath string, logger *slog.Logger) (Runnable, error)
	// Subcommands are optional one-shot subcommands (e.g. "authorize"),
	// dispatched before flag parsing. The subcommand name MUST be the first
	// argument: "snooze-foo -c x.yaml authorize" will NOT dispatch. Each
	// returns a process exit code.
	Subcommands map[string]func(args []string) int
}

// Main is the binary entry point. It never returns.
func Main(c Config) { os.Exit(run(os.Args[1:], c, os.Stdout, os.Stderr)) }

// run is the testable core. Exit codes: 0 clean (incl. signal-driven
// context.Canceled); 2 usage error (bad/missing flag); 1 runtime error
// (config load, build, or run failure).
func run(args []string, c Config, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "version" {
		_, _ = fmt.Fprintln(stdout, c.Name, version.String())
		return 0
	}
	if len(args) > 0 {
		if sub, ok := c.Subcommands[args[0]]; ok {
			return sub(args[1:])
		}
	}

	fs := flag.NewFlagSet(c.Name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	cfgPath := fs.String("c", c.DefaultConfig, "path to the YAML config file")
	debug := fs.Bool("debug", false, "enable debug logging")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	logger := newLogger(*debug, stderr)
	slog.SetDefault(logger)

	runnable, err := c.Build(*cfgPath, logger)
	if err != nil {
		logger.Error(c.Name+": startup failed", slog.Any("err", err))
		return 1
	}
	if runnable == nil {
		logger.Error(c.Name + ": Build returned a nil Runnable without an error")
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Only context.Canceled (signal-driven shutdown) is a clean exit; any
	// other error, including context.DeadlineExceeded, is a failure.
	if err := runnable.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		logger.Error(c.Name+": exited with error", slog.Any("err", err))
		return 1
	}
	return 0
}
