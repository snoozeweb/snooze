// Command snooze-pacemaker is a one-shot Pacemaker fence helper.
//
// Pacemaker (or any other stonith-ng compatible cluster manager) calls this
// binary with a single "action" argument — metadata / monitor / list / status
// for probes, or on / off / reboot when a node must actually be fenced. The
// helper does NOT perform any fencing itself; its job is to forward a single
// alert record to the Snooze v1 alert API so the operator has an audit trail
// of every fence event.
//
// Usage:
//
//	snooze-pacemaker [-config /etc/snooze/pacemaker.yaml] <action> [node]
//	snooze-pacemaker version
//
// Parameters can be supplied via positional args or via environment variables
// (the convention preferred by stonith-ng): `action`, `nodename`/`port`, and
// `reason`. Credentials come from $SNOOZE_SERVER / $SNOOZE_USERNAME /
// $SNOOZE_PASSWORD (or the YAML file).
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/snoozeweb/snooze/internal/components/pacemaker"
	"github.com/snoozeweb/snooze/internal/version"

	// Blank-imported so GOMAXPROCS auto-tunes to the container CPU quota.
	_ "github.com/snoozeweb/snooze/internal/runtime"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Println("snooze-pacemaker", version.String())
		return
	}

	fs := flag.NewFlagSet("snooze-pacemaker", flag.ExitOnError)
	cfgPath := fs.String("config", defaultConfigPath(), "path to pacemaker.yaml")
	logLevel := fs.String("log-level", "info", "log level: debug|info|warn|error")
	if err := fs.Parse(os.Args[1:]); err != nil {
		os.Exit(2)
	}

	logger := newLogger(*logLevel)
	slog.SetDefault(logger)

	cfg, err := pacemaker.LoadConfig(*cfgPath)
	if err != nil {
		logger.Error("snooze-pacemaker: load config", slog.Any("err", err))
		os.Exit(1)
	}

	runner := pacemaker.NewRunner(pacemaker.Options{
		Config: cfg,
		Logger: logger,
	})
	code, _ := runner.Run(context.Background(), fs.Args())
	os.Exit(code)
}

// defaultConfigPath returns the config location, honouring SNOOZE_PACEMAKER_CONFIG.
func defaultConfigPath() string {
	if p := os.Getenv("SNOOZE_PACEMAKER_CONFIG"); p != "" {
		return p
	}
	return pacemaker.DefaultConfigPath
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
