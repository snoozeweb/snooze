// Command snooze-teams bridges Microsoft Teams with snooze-server.
//
// Usage:
//
//	snooze-teams              # use /etc/snooze/teams.yaml
//	snooze-teams -c path.yaml # explicit config
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

	"github.com/japannext/snooze/internal/components/teams"
	"github.com/japannext/snooze/internal/version"

	// Blank-imported so GOMAXPROCS auto-tunes to the container CPU quota.
	_ "github.com/japannext/snooze/internal/runtime"
)

// defaultConfigPath is the systemd-friendly location packaging will install
// to. The -c flag overrides it.
const defaultConfigPath = "/etc/snooze/teams.yaml"

func run() int {
	cfgPath := flag.String("c", defaultConfigPath, "path to teams.yaml")
	debug := flag.Bool("debug", false, "enable debug logging")
	flag.Parse()

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

func main() {
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Println("snooze-teams", version.String())
		return
	}
	os.Exit(run())
}
