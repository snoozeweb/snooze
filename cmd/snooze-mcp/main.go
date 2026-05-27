// Command snooze-mcp is a Model Context Protocol (MCP) server that exposes
// Snooze alerts and record actions as tools to AI assistants.
//
// It speaks JSON-RPC 2.0 over stdio (newline-delimited messages on
// stdin/stdout) and is spawned on-demand by an MCP client (Claude Desktop,
// Cursor, …) — it is NOT a long-running network service and has no systemd
// unit. All logging goes to stderr because stdout carries the protocol.
//
// Configuration comes from /etc/snooze/mcp.yaml and/or the environment
// (SNOOZE_SERVER, SNOOZE_TOKEN, SNOOZE_USERNAME, …); the environment wins so
// an MCP client launch block is authoritative. A missing config file is fine
// when the required fields are supplied via the environment.
//
// Usage:
//
//	snooze-mcp              # use /etc/snooze/mcp.yaml (+ env overrides)
//	snooze-mcp -c path.yaml # explicit config
//	snooze-mcp version      # print build info and exit
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

	"github.com/snoozeweb/snooze/internal/components/mcp"
	"github.com/snoozeweb/snooze/internal/version"

	// Blank-imported so GOMAXPROCS auto-tunes to the container CPU quota.
	_ "github.com/snoozeweb/snooze/internal/runtime"
)

// defaultConfigPath is the systemd-friendly location packaging will install
// to. The -c flag overrides it. Missing-file is tolerated (env-only launch).
const defaultConfigPath = "/etc/snooze/mcp.yaml"

func run() int {
	cfgPath := flag.String("c", defaultConfigPath, "path to mcp.yaml")
	debug := flag.Bool("debug", false, "enable debug logging")
	flag.Parse()

	cfg, err := mcp.LoadConfig(*cfgPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "snooze-mcp:", err)
		return 2
	}

	level := slog.LevelInfo
	if *debug || cfg.Debug {
		level = slog.LevelDebug
	}
	// IMPORTANT: stderr only — stdout is the JSON-RPC protocol channel.
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)

	d, err := mcp.New(cfg, logger)
	if err != nil {
		fmt.Fprintln(os.Stderr, "snooze-mcp:", err)
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := d.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, "snooze-mcp:", err)
		return 1
	}
	return 0
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Println("snooze-mcp", version.String())
		return
	}
	os.Exit(run())
}
