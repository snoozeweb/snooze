// Command snooze-otlp runs the OpenTelemetry OTLP/HTTP (JSON) receiver that
// converts OTLP log records into Snooze v1 alerts.
//
// It speaks OTLP/HTTP for the logs signal only (POST /v1/logs, Content-Type
// application/json, optionally gzip-compressed). gRPC OTLP and binary-Protobuf
// encoding are NOT supported — the daemon is HTTP + JSON only.
//
// Usage:
//
//	snooze-otlp              # use /etc/snooze/otlp.yaml
//	snooze-otlp -c path.yaml # explicit config
//	snooze-otlp version      # print build info and exit
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

	"github.com/snoozeweb/snooze/internal/components/otlp"
	"github.com/snoozeweb/snooze/internal/version"

	// Blank-imported so GOMAXPROCS auto-tunes to the container CPU quota.
	_ "github.com/snoozeweb/snooze/internal/runtime"
)

// defaultConfigPath is the systemd-friendly location packaging installs to.
const defaultConfigPath = "/etc/snooze/otlp.yaml"

func run() int {
	cfgPath := flag.String("c", defaultConfigPath, "path to otlp.yaml")
	debug := flag.Bool("debug", false, "enable debug logging")
	flag.Parse()

	cfg, err := otlp.LoadConfig(*cfgPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "snooze-otlp:", err)
		return 2
	}

	level := slog.LevelInfo
	if *debug || cfg.Debug {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)

	d, err := otlp.New(cfg, logger)
	if err != nil {
		fmt.Fprintln(os.Stderr, "snooze-otlp:", err)
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := d.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, "snooze-otlp:", err)
		return 1
	}
	return 0
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Println("snooze-otlp", version.String())
		return
	}
	os.Exit(run())
}
