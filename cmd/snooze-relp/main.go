// Command snooze-relp ingests RELP-framed syslog and forwards to snooze-server.
package main

import (
	"fmt"
	"log/slog"

	"github.com/snoozeweb/snooze/internal/components/relp"
	"github.com/snoozeweb/snooze/internal/daemon"
	"github.com/snoozeweb/snooze/pkg/snoozeclient"
)

func main() {
	daemon.Main(daemon.Config{
		Name:          "snooze-relp",
		DefaultConfig: daemon.EnvOr("SNOOZE_RELP_CONFIG", "/etc/snooze/relp.yaml"),
		Build: func(cfgPath string, log *slog.Logger) (daemon.Runnable, error) {
			cfg, err := relp.LoadConfig(cfgPath)
			if err != nil {
				return nil, err
			}
			client, err := snoozeclient.New(snoozeclient.Options{
				BaseURL:  cfg.Server,
				Username: cfg.Username,
				Password: cfg.Password,
				Method:   cfg.Method,
				Token:    cfg.Token,
				Insecure: cfg.Insecure,
				Timeout:  cfg.RequestTimeout,
				Logger:   log.With(slog.String("component", "snoozeclient")),
			})
			if err != nil {
				return nil, fmt.Errorf("build client: %w", err)
			}
			d, err := relp.New(relp.Options{Config: cfg, Client: client, Logger: log})
			if err != nil {
				return nil, err
			}
			return d, nil
		},
	})
}
