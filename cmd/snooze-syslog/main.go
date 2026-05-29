// Command snooze-syslog ingests syslog (RFC3164/5424) over UDP and/or TCP and
// forwards each parsed message to a Snooze server's /api/v1/alerts endpoint.
package main

import (
	"log/slog"

	"github.com/snoozeweb/snooze/internal/components/syslog"
	"github.com/snoozeweb/snooze/internal/daemon"
)

func main() {
	daemon.Main(daemon.Config{
		Name:          "snooze-syslog",
		DefaultConfig: "/etc/snooze/syslog.yaml",
		Build: func(cfgPath string, log *slog.Logger) (daemon.Runnable, error) {
			cfg, err := syslog.LoadConfig(cfgPath)
			if err != nil {
				return nil, err
			}
			d, err := syslog.New(cfg, log)
			if err != nil {
				return nil, err
			}
			return d, nil
		},
	})
}
