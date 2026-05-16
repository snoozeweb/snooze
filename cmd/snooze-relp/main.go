// Command snooze-relp ingests RELP-framed syslog and forwards to snooze-server.
//
// Usage:
//
//	snooze-relp [-config /etc/snooze/relp.yaml]
//	snooze-relp version
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

	"github.com/snoozeweb/snooze/internal/components/relp"
	"github.com/snoozeweb/snooze/internal/version"
	"github.com/snoozeweb/snooze/pkg/snoozeclient"

	// Blank-imported so GOMAXPROCS auto-tunes to the container CPU quota.
	_ "github.com/snoozeweb/snooze/internal/runtime"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Println("snooze-relp", version.String())
		return
	}

	fs := flag.NewFlagSet("snooze-relp", flag.ExitOnError)
	cfgPath := fs.String("config", defaultConfigPath(), "path to relp.yaml")
	logLevel := fs.String("log-level", "info", "log level: debug|info|warn|error")
	if err := fs.Parse(os.Args[1:]); err != nil {
		os.Exit(2)
	}

	logger := newLogger(*logLevel)
	slog.SetDefault(logger)

	if err := run(*cfgPath, logger); err != nil {
		logger.Error("snooze-relp: fatal", slog.Any("err", err))
		os.Exit(1)
	}
}

// run is the testable entrypoint. It loads config, builds the snooze client,
// constructs a Daemon and serves until SIGINT/SIGTERM.
func run(cfgPath string, logger *slog.Logger) error {
	cfg, err := relp.LoadConfig(cfgPath)
	if err != nil {
		return err
	}

	client, err := snoozeclient.New(snoozeclient.Options{
		BaseURL:  cfg.Server,
		Username: cfg.Username,
		Password: cfg.Password,
		Method:   cfg.Method,
		Token:    cfg.Token,
		Insecure: cfg.Insecure,
		Timeout:  cfg.RequestTimeout,
		Logger:   logger.With(slog.String("component", "snoozeclient")),
	})
	if err != nil {
		return fmt.Errorf("snooze-relp: build client: %w", err)
	}

	d, err := relp.New(relp.Options{
		Config: cfg,
		Client: client,
		Logger: logger,
	})
	if err != nil {
		return err
	}

	ctx, cancel := signal.NotifyContext(context.Background(),
		syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := d.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	logger.Info("snooze-relp: shutdown complete")
	return nil
}

// defaultConfigPath returns the config location, honouring the
// SNOOZE_RELP_CONFIG env var to match the Python daemon's behaviour.
func defaultConfigPath() string {
	if p := os.Getenv("SNOOZE_RELP_CONFIG"); p != "" {
		return p
	}
	return "/etc/snooze/relp.yaml"
}

// newLogger builds a JSON slog Logger at the requested level. Unknown levels
// fall back to info.
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
	return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: lvl}))
}
