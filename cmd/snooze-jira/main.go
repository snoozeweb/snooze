// Command snooze-jira is the JIRA Cloud output bridge: it exposes an HTTP
// webhook that snooze-server hits, creating/commenting JIRA issues and
// optionally closing records when tickets transition to Done.
package main

import (
	"log/slog"

	"github.com/snoozeweb/snooze/internal/components/jira"
	"github.com/snoozeweb/snooze/internal/daemon"
)

func main() {
	daemon.Main(daemon.Config{
		Name:          "snooze-jira",
		DefaultConfig: "/etc/snooze/jira.yaml",
		Build: func(cfgPath string, log *slog.Logger) (daemon.Runnable, error) {
			cfg, err := jira.LoadConfig(cfgPath)
			if err != nil {
				return nil, err
			}
			d, err := jira.New(cfg, log)
			if err != nil {
				return nil, err
			}
			return d, nil
		},
	})
}
