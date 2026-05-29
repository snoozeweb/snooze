// Command snooze-smtp accepts mail over SMTP and forwards each message to a
// Snooze server's /api/v1/alerts endpoint.
package main

import (
	"log/slog"

	"github.com/snoozeweb/snooze/internal/components/smtp"
	"github.com/snoozeweb/snooze/internal/daemon"
)

func main() {
	daemon.Main(daemon.Config{
		Name:          "snooze-smtp",
		DefaultConfig: "/etc/snooze/smtp.yaml",
		Build: func(cfgPath string, log *slog.Logger) (daemon.Runnable, error) {
			cfg, err := smtp.LoadConfig(cfgPath)
			if err != nil {
				return nil, err
			}
			d, err := smtp.NewDaemon(cfg, log)
			if err != nil {
				return nil, err
			}
			return d, nil
		},
	})
}
