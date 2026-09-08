// Package notificationlog implements the "notificationlog" data-model plugin:
// the per-send delivery history an operator reads as "Deliveries" in the UI.
//
// One row = one actual send attempt by one action, covering 1..N alerts. An
// unbatched send writes one row per (alert x action); a batching notifier
// writes a single row per flush listing every member alert. Rows are produced
// out-of-band by the notification dispatcher and the batching notifiers via
// plugins.RecordDelivery — the plugin only owns the schema and the read-side
// CRUD surface (GET/POST/DELETE /api/v1/notificationlog, /search, /{uid},
// the ?q= DSL and the ro_/rw_notificationlog permissions).
//
// The collection is deliberately NOT registered global: delivery rows are
// tenant-scoped like the alerts they describe. Retention is the housekeeper's
// cleanup_notificationlog job, keyed on date_epoch.
package notificationlog

import (
	"context"
	_ "embed"
	"errors"

	"github.com/snoozeweb/snooze/internal/plugins"
)

//go:embed metadata.yaml
var metaYAML []byte

func init() {
	plugins.Register(plugins.NotificationLogCollection, metaYAML, factory)
}

func factory(meta plugins.Metadata) (plugins.Plugin, error) {
	return &Plugin{meta: meta}, nil
}

// Plugin is the data-model plugin for delivery-history rows.
type Plugin struct {
	meta plugins.Metadata
	host plugins.Host
}

// Name returns the registered plugin name and collection identifier.
func (p *Plugin) Name() string { return plugins.NotificationLogCollection }

// Metadata returns the parsed metadata.yaml descriptor.
func (p *Plugin) Metadata() plugins.Metadata { return p.meta }

// PostInit captures the host for subsequent calls.
func (p *Plugin) PostInit(_ context.Context, host plugins.Host) error {
	p.host = host
	return nil
}

// Reload is a no-op: delivery rows are history, never cached config.
func (p *Plugin) Reload(_ context.Context) error { return nil }

// Schema returns the JSON Schema for one delivery-history row. It mirrors the
// `NotificationLogEntry` component in api/openapi.yaml and the document
// produced by plugins.DeliveryRow.Doc().
//
// The flat `*_uids` / `*_hashes` / `*_names` arrays are duplicated out of
// `alerts[]` on purpose: the search DSL can express `field CONTAINS "x"` on a
// JSON array but cannot filter on an object path inside one, so every UI
// filter (deliveries for a notification, for an alert) needs the flat form.
func (p *Plugin) Schema() any {
	stringArray := map[string]any{
		"type":  "array",
		"items": map[string]any{"type": "string"},
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			// When.
			"date_epoch":   map[string]any{"type": "number"},
			"queued_epoch": map[string]any{"type": "number"},
			"duration_ms":  map[string]any{"type": "integer"},

			// Outcome.
			"status": map[string]any{
				"type": "string",
				"enum": []any{plugins.DeliveryStatusSuccess, plugins.DeliveryStatusError},
			},
			"error": map[string]any{"type": "string"},

			// What sent it.
			"action":   map[string]any{"type": "string"},
			"notifier": map[string]any{"type": "string"},
			"batch":    map[string]any{"type": "boolean"},
			"batch_reason": map[string]any{
				"type": "string",
				"enum": []any{"", plugins.BatchReasonSize, plugins.BatchReasonTimer, plugins.BatchReasonShutdown},
			},

			// Which notifications routed it.
			"notification_uids":  stringArray,
			"notification_names": stringArray,

			// Which alerts it covered.
			"alert_count":  map[string]any{"type": "integer"},
			"alert_uids":   stringArray,
			"alert_hashes": stringArray,
			"alerts": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"uid":          map[string]any{"type": "string"},
						"hash":         map[string]any{"type": "string"},
						"host":         map[string]any{"type": "string"},
						"severity":     map[string]any{"type": "string"},
						"message":      map[string]any{"type": "string"},
						"state":        map[string]any{"type": "string"},
						"notification": map[string]any{"type": "string"},
					},
					"additionalProperties": true,
				},
			},

			// Re-escalation context.
			"escalation_count":  map[string]any{"type": "integer"},
			"escalation_reason": map[string]any{"type": "string"},

			// Optional external handle (jira issue key, slack thread, …).
			"ref": map[string]any{"type": "object", "additionalProperties": true},
		},
		"additionalProperties": true,
	}
}

// Validate accepts any well-formed map. The writer is the dispatcher, not an
// operator form, and rejecting a row here would drop history for a send that
// already happened.
func (p *Plugin) Validate(_ map[string]any) error { return nil }

// ErrNoHTTPWrites is returned by GuardWrite for every create/replace/patch
// arriving over HTTP.
var ErrNoHTTPWrites = errors.New(
	"notificationlog rows are written by the notification dispatcher; " +
		"the HTTP create/update endpoints are not a supported way to add delivery history")

// GuardWrite refuses every HTTP create (POST), replace (PUT) and merge (PATCH)
// on this collection with 403.
//
// Registering a data-model plugin buys the whole CRUD surface, and for a
// delivery LOG the write half is not a convenience — it is a forgery
// primitive. Nothing legitimate writes here over HTTP: rows come from
// plugins.RecordDelivery inside the dispatcher, which goes straight to the
// driver and never through this handler chain. Left mounted, anyone holding
// rw_notificationlog could fabricate a "delivery" that never happened, or
// rewrite a failed one as successful — and because metadata.yaml sets
// `audit: false` (rows are too high-volume to audit), the forgery would leave
// no trace at all.
//
// DELETE is deliberately still allowed (it needs rw_notificationlog): the e2e
// harness purges the collection between specs, and operators need a way to
// drop history ahead of the retention sweep. Deleting history is destructive
// but not deceptive.
func (p *Plugin) GuardWrite(_ context.Context, _ string, _ map[string]any, _ bool) error {
	return ErrNoHTTPWrites
}
