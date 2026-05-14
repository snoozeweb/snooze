// Command snooze-mattermost bridges Mattermost with snooze-server.
//
// The daemon connects to a Mattermost server over WebSocket, listens for
// slash-command / mention events in configured channels, translates them
// into Snooze REST API calls (ack/close/reopen/comment) and posts a reply
// back into Mattermost.
//
// Usage:
//
//	snooze-mattermost [-config /etc/snooze/mattermost.yaml]
//	snooze-mattermost version
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

	"github.com/japannext/snooze/internal/components/mattermost"
	"github.com/japannext/snooze/internal/version"

	// Blank-imported so GOMAXPROCS auto-tunes to the container CPU quota.
	_ "github.com/japannext/snooze/internal/runtime"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Println("snooze-mattermost", version.String())
		return
	}

	fs := flag.NewFlagSet("snooze-mattermost", flag.ExitOnError)
	configPath := fs.String("config", os.Getenv("SNOOZE_MATTERMOST_CONFIG"), "path to YAML config file")
	debug := fs.Bool("debug", false, "enable debug logging")
	if err := fs.Parse(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	level := slog.LevelInfo
	if *debug {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)

	if *configPath == "" {
		fmt.Fprintln(os.Stderr, "snooze-mattermost: -config is required (or set SNOOZE_MATTERMOST_CONFIG)")
		os.Exit(2)
	}

	cfg, err := mattermost.LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "snooze-mattermost: load config: %v\n", err)
		os.Exit(1)
	}

	daemon, err := mattermost.NewDaemon(cfg, mattermost.WithLogger(logger))
	if err != nil {
		fmt.Fprintf(os.Stderr, "snooze-mattermost: build daemon: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := daemon.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintf(os.Stderr, "snooze-mattermost: %v\n", err)
		os.Exit(1)
	}
}
