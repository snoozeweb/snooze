// Package all blank-imports every Snooze plugin package so that each
// plugin's init() runs and registers itself with internal/plugins.
//
// Binaries that need the full plugin set (today: snooze-server) blank-import
// this package once from their main. Binaries that ship without plugins (the
// CLI, the component daemons) do not import it.
package all

import (
	_ "github.com/japannext/snooze/internal/pluginimpl/action"
	_ "github.com/japannext/snooze/internal/pluginimpl/aggregaterule"
	_ "github.com/japannext/snooze/internal/pluginimpl/alertmanager"
	_ "github.com/japannext/snooze/internal/pluginimpl/audit"
	_ "github.com/japannext/snooze/internal/pluginimpl/comment"
	_ "github.com/japannext/snooze/internal/pluginimpl/environment"
	_ "github.com/japannext/snooze/internal/pluginimpl/grafana"
	_ "github.com/japannext/snooze/internal/pluginimpl/influxdb2"
	_ "github.com/japannext/snooze/internal/pluginimpl/kapacitor"
	_ "github.com/japannext/snooze/internal/pluginimpl/kv"
	_ "github.com/japannext/snooze/internal/pluginimpl/mail"
	_ "github.com/japannext/snooze/internal/pluginimpl/notification"
	_ "github.com/japannext/snooze/internal/pluginimpl/patlite"
	_ "github.com/japannext/snooze/internal/pluginimpl/profile"
	_ "github.com/japannext/snooze/internal/pluginimpl/prometheus"
	_ "github.com/japannext/snooze/internal/pluginimpl/record"
	_ "github.com/japannext/snooze/internal/pluginimpl/role"
	_ "github.com/japannext/snooze/internal/pluginimpl/rule"
	_ "github.com/japannext/snooze/internal/pluginimpl/script"
	_ "github.com/japannext/snooze/internal/pluginimpl/settings"
	_ "github.com/japannext/snooze/internal/pluginimpl/snooze"
	_ "github.com/japannext/snooze/internal/pluginimpl/stats"
	_ "github.com/japannext/snooze/internal/pluginimpl/user"
	_ "github.com/japannext/snooze/internal/pluginimpl/webhook"
	_ "github.com/japannext/snooze/internal/pluginimpl/widget"
)
