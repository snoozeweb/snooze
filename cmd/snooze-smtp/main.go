// Command snooze-smtp runs the inbound SMTP daemon that converts received
// mail into Snooze v1 alerts.
//
// The binary takes a single mandatory flag -config pointing at a YAML file
// (see internal/components/smtp.Config). All other knobs come from that file.
//
//	snooze-smtp -config /etc/snooze/smtp.yaml
//	snooze-smtp version
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

	"github.com/snoozeweb/snooze/internal/components/smtp"
	"github.com/snoozeweb/snooze/internal/version"

	// Blank-imported so GOMAXPROCS auto-tunes to the container CPU quota.
	_ "github.com/snoozeweb/snooze/internal/runtime"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Println("snooze-smtp", version.String())
		return
	}

	fs := flag.NewFlagSet("snooze-smtp", flag.ContinueOnError)
	configPath := fs.String("config", "/etc/snooze/smtp.yaml", "Path to the YAML config file.")
	logLevel := fs.String("log-level", "info", "Log verbosity (debug|info|warn|error).")
	if err := fs.Parse(os.Args[1:]); err != nil {
		os.Exit(2)
	}

	logger := newLogger(*logLevel)

	cfg, err := smtp.LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "snooze-smtp: load config:", err)
		os.Exit(1)
	}

	daemon, err := smtp.NewDaemon(cfg, logger)
	if err != nil {
		fmt.Fprintln(os.Stderr, "snooze-smtp: init daemon:", err)
		os.Exit(1)
	}
	if err := daemon.Listen(); err != nil {
		fmt.Fprintln(os.Stderr, "snooze-smtp: listen:", err)
		os.Exit(1)
	}

	os.Exit(runSMTP(daemon))
}

func runSMTP(daemon *smtp.Daemon) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := daemon.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, "snooze-smtp:", err)
		return 1
	}
	return 0
}

// newLogger maps a string log level to a slog.Logger writing to stderr.
func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lvl}))
}
