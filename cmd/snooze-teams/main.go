// Command snooze-teams bridges Microsoft Teams with snooze-server.
//
// Usage:
//
//	snooze-teams              # use /etc/snooze/teams.yaml
//	snooze-teams -c path.yaml # explicit config
//	snooze-teams authorize    # one-shot OAuth2 device-code flow
//	snooze-teams version      # print build info and exit
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/snoozeweb/snooze/internal/components/teams"
	"github.com/snoozeweb/snooze/internal/version"

	// Blank-imported so GOMAXPROCS auto-tunes to the container CPU quota.
	_ "github.com/snoozeweb/snooze/internal/runtime"
)

// defaultConfigPath is the systemd-friendly location packaging will install
// to. The -c flag overrides it.
const defaultConfigPath = "/etc/snooze/teams.yaml"

// runDaemon parses the daemon flags and drives Daemon.Run until shutdown.
func runDaemon(args []string) int {
	fs := flag.NewFlagSet("snooze-teams", flag.ContinueOnError)
	cfgPath := fs.String("c", defaultConfigPath, "path to teams.yaml")
	debug := fs.Bool("debug", false, "enable debug logging")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	level := slog.LevelInfo
	if *debug {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)

	cfg, err := teams.LoadConfig(*cfgPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "snooze-teams:", err)
		return 2
	}

	d, err := teams.New(cfg, logger)
	if err != nil {
		fmt.Fprintln(os.Stderr, "snooze-teams:", err)
		return 2
	}

	// signal.NotifyContext gives us a context that cancels on the first
	// SIGINT/SIGTERM; the Daemon's Run loop watches ctx.Done().
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := d.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, "snooze-teams:", err)
		return 1
	}
	return 0
}

// runAuthorize parses the authorize subcommand flags and runs the OAuth2
// device-code flow, persisting the resulting refresh token to disk.
func runAuthorize(args []string) int {
	fs := flag.NewFlagSet("snooze-teams authorize", flag.ContinueOnError)
	cfgPath := fs.String("c", defaultConfigPath, "path to teams.yaml")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := teams.LoadConfig(*cfgPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "snooze-teams: load config:", err)
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := teams.Authorize(ctx, cfg, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "snooze-teams: authorize:", err)
		return 1
	}
	return 0
}

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "version":
			fmt.Println("snooze-teams", version.String())
			return
		case "authorize":
			os.Exit(runAuthorize(os.Args[2:]))
		}
	}
	os.Exit(runDaemon(os.Args[1:]))
}
