// Command snooze-mattermost bridges Mattermost with snooze-server over
// WebSocket, translating slash-commands into Snooze REST calls.
package main

import (
	"log/slog"

	"github.com/snoozeweb/snooze/internal/components/mattermost"
	"github.com/snoozeweb/snooze/internal/daemon"
)

func main() {
	daemon.Main(daemon.Config{
		Name:          "snooze-mattermost",
		DefaultConfig: daemon.EnvOr("SNOOZE_MATTERMOST_CONFIG", "/etc/snooze/mattermost.yaml"),
		Build: func(cfgPath string, log *slog.Logger) (daemon.Runnable, error) {
			cfg, err := mattermost.LoadConfig(cfgPath)
			if err != nil {
				return nil, err
			}
			d, err := mattermost.NewDaemon(cfg, mattermost.WithLogger(log))
			if err != nil {
				return nil, err
			}
			return d, nil
		},
	})
}
