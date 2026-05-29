// Command snooze-snmptrap receives SNMP traps and forwards them to a Snooze
// server's /api/v1/alerts endpoint.
package main

import (
	"log/slog"

	"github.com/snoozeweb/snooze/internal/components/snmptrap"
	"github.com/snoozeweb/snooze/internal/daemon"
)

func main() {
	daemon.Main(daemon.Config{
		Name:          "snooze-snmptrap",
		DefaultConfig: "/etc/snooze/snmptrap.yaml",
		Build: func(cfgPath string, log *slog.Logger) (daemon.Runnable, error) {
			cfg, err := snmptrap.LoadConfig(cfgPath)
			if err != nil {
				return nil, err
			}
			d, err := snmptrap.NewDaemon(cfg, log)
			if err != nil {
				return nil, err
			}
			return d, nil
		},
	})
}
