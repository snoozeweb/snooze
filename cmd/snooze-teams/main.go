// Command snooze-teams bridges Microsoft Teams with snooze-server. The
// `authorize` subcommand runs a one-shot OAuth2 device-code flow.
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/snoozeweb/snooze/internal/components/teams"
	"github.com/snoozeweb/snooze/internal/daemon"
)

func main() {
	daemon.Main(daemon.Config{
		Name:          "snooze-teams",
		DefaultConfig: "/etc/snooze/teams.yaml",
		Build: func(cfgPath string, log *slog.Logger) (daemon.Runnable, error) {
			cfg, err := teams.LoadConfig(cfgPath)
			if err != nil {
				return nil, err
			}
			d, err := teams.New(cfg, log)
			if err != nil {
				return nil, err
			}
			return d, nil
		},
		Subcommands: map[string]func(args []string) int{
			"authorize": authorize,
		},
	})
}

// authorize runs the OAuth2 device-code flow, persisting the refresh token.
func authorize(args []string) int {
	fs := flag.NewFlagSet("snooze-teams authorize", flag.ContinueOnError)
	cfgPath := fs.String("c", "/etc/snooze/teams.yaml", "path to teams.yaml")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	logger := daemon.NewLogger(false)
	slog.SetDefault(logger)

	cfg, err := teams.LoadConfig(*cfgPath)
	if err != nil {
		logger.Error("snooze-teams: authorize: load config", slog.Any("err", err))
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := teams.Authorize(ctx, cfg, os.Stderr); err != nil {
		logger.Error("snooze-teams: authorize", slog.Any("err", err))
		return 1
	}
	return 0
}
