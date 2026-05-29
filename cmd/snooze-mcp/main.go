// Command snooze-mcp is a Model Context Protocol server exposing Snooze alerts
// and record actions as tools to AI assistants over JSON-RPC 2.0 on stdio.
// All logging goes to stderr because stdout carries the protocol.
package main

import (
	"log/slog"

	"github.com/snoozeweb/snooze/internal/components/mcp"
	"github.com/snoozeweb/snooze/internal/daemon"
)

func main() {
	daemon.Main(daemon.Config{
		Name:          "snooze-mcp",
		DefaultConfig: "/etc/snooze/mcp.yaml",
		Build: func(cfgPath string, log *slog.Logger) (daemon.Runnable, error) {
			cfg, err := mcp.LoadConfig(cfgPath)
			if err != nil {
				return nil, err
			}
			d, err := mcp.New(cfg, log)
			if err != nil {
				return nil, err
			}
			return d, nil
		},
	})
}
