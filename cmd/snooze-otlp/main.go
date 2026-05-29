// Command snooze-otlp runs the OpenTelemetry OTLP/HTTP (JSON) receiver that
// converts OTLP log records into Snooze v1 alerts (POST /v1/logs).
package main

import (
	"log/slog"

	"github.com/snoozeweb/snooze/internal/components/otlp"
	"github.com/snoozeweb/snooze/internal/daemon"
)

func main() {
	daemon.Main(daemon.Config{
		Name:          "snooze-otlp",
		DefaultConfig: "/etc/snooze/otlp.yaml",
		Build: func(cfgPath string, log *slog.Logger) (daemon.Runnable, error) {
			cfg, err := otlp.LoadConfig(cfgPath)
			if err != nil {
				return nil, err
			}
			d, err := otlp.New(cfg, log)
			if err != nil {
				return nil, err
			}
			return d, nil
		},
	})
}
