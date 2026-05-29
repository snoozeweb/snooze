// Command snooze-k8s-events watches the Kubernetes Event API and forwards
// events to a Snooze server's /api/v1/alerts endpoint.
package main

import (
	"log/slog"

	"github.com/snoozeweb/snooze/internal/components/k8sevents"
	"github.com/snoozeweb/snooze/internal/daemon"
)

func main() {
	daemon.Main(daemon.Config{
		Name:          "snooze-k8s-events",
		DefaultConfig: "/etc/snooze/k8s-events.yaml",
		Build: func(cfgPath string, log *slog.Logger) (daemon.Runnable, error) {
			cfg, err := k8sevents.LoadConfig(cfgPath)
			if err != nil {
				return nil, err
			}
			d, err := k8sevents.New(cfg, log)
			if err != nil {
				return nil, err
			}
			return d, nil
		},
	})
}
