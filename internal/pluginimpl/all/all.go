// Package all blank-imports every Snooze plugin package so that each
// plugin's init() runs and registers itself with internal/plugins.
//
// Binaries that need the full plugin set (today: snooze-server) blank-import
// this package once from their main. Binaries that ship without plugins (the
// CLI, the component daemons) do not import it.
package all

// Register all built-in plugins by importing their init functions.
import (
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/action"
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/aggregaterule"
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/alertmanager"
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/audit"
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/comment"
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/environment"
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/grafana"
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/influxdb2"
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/kapacitor"
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/kv"
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/mail"
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/notification"
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/patlite"
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/profile"
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/prometheus"
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/record"
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/role"
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/rule"
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/script"
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/settings"
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/snooze"
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/stats"
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/user"
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/webhook"
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/widget"
)
