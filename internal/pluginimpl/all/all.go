// Package all blank-imports every Snooze plugin package so that each
// plugin's init() runs and registers itself with internal/plugins.
//
// Binaries that need the full plugin set (today: snooze-server) blank-import
// this package once from their main. Binaries that ship without plugins (the
// CLI, the component daemons) do not import it.
//
// Imports are grouped by the plugin's primary role (see internal/pluginimpl/
// AGENTS.md for the taxonomy). The grouping is documentation only — every
// plugin registers itself identically regardless of which group it sits in.
package all

import (
	// Data models — CRUD-able config/state collections.
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/action"
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/apikey"
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/audit"
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/comment"
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/environment"
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/kv"
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/profile"
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/record"
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/role"
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/settings"
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/stats"
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/tenant"
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/user"
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/widget"

	// Pipeline processors — transform / gate alerts as they flow through.
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/notification"
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/rule"
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/snooze"

	// Notifiers — outbound delivery to an external destination.
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/discord"
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/googlechat"
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/mail"
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/mattermost"
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/ntfy"
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/opsgenie"
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/pagerduty"
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/patlite"
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/pushover"
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/script"
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/servicenow"
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/slack"
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/sns"
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/statuspage"
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/teams"
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/telegram"
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/twilio"
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/webhook"

	// Webhook receivers — inbound HTTP from external monitoring sources,
	// mounted under /api/v1/webhook/{name}.
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/alertmanager"
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/azuremonitor"
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/cloudwatch"
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/datadog"
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/grafana"
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/influxdb2"
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/kapacitor"
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/newrelic"
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/prometheus"
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/sentry"

	// Multi-role — implement more than one of the roles above.
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/aggregaterule" // data model + pipeline processor
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/heartbeat"     // data model + inbound webhook + lifecycle
)
