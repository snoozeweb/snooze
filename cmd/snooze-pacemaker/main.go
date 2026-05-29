// Command snooze-pacemaker is a one-shot Pacemaker fence helper. stonith-ng
// calls it with an action argument; it forwards a single alert record to the
// Snooze v1 alert API for an audit trail (it does not perform fencing itself).
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"

	"github.com/snoozeweb/snooze/internal/components/pacemaker"
	"github.com/snoozeweb/snooze/internal/daemon"
)

func main() {
	if daemon.HandleVersion("snooze-pacemaker", os.Args[1:], os.Stdout) {
		return
	}

	fs := flag.NewFlagSet("snooze-pacemaker", flag.ContinueOnError)
	cfgPath := fs.String("c", daemon.EnvOr("SNOOZE_PACEMAKER_CONFIG", pacemaker.DefaultConfigPath), "path to pacemaker.yaml")
	debug := fs.Bool("debug", false, "enable debug logging")
	if err := fs.Parse(os.Args[1:]); err != nil {
		os.Exit(2)
	}

	logger := daemon.NewLogger(*debug)
	slog.SetDefault(logger)

	cfg, err := pacemaker.LoadConfig(*cfgPath)
	if err != nil {
		logger.Error("snooze-pacemaker: load config", slog.Any("err", err))
		os.Exit(1)
	}

	runner := pacemaker.NewRunner(pacemaker.Options{Config: cfg, Logger: logger})
	code, _ := runner.Run(context.Background(), fs.Args())
	os.Exit(code)
}
