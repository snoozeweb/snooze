package core

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/internal/protected"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// recordCollection is the persistence target for processed alerts.
const recordCollection = "record"

// ProcessRecord walks rec through the configured processor pipeline and
// returns the final record, the terminal Action, and any error.
//
// Semantics per plugin verdict:
//
//   - ActionContinue: rec becomes Result.Record; the next plugin runs.
//   - ActionAbort: stop and return the record without persisting.
//   - ActionAbortWrite: persist rec with a fresh updated_at and return.
//   - ActionAbortUpdate: persist rec without bumping updated_at and return.
//
// If a plugin returns an error, the record gets an “exception“ field
// describing it, the record is written for forensic reasons, and Abort is
// returned along with the wrapped error.
//
// When every plugin votes Continue, the record is persisted and
// ActionContinue is returned to the caller.
func (c *Core) ProcessRecord(ctx context.Context, rec snoozetypes.Record) (snoozetypes.Record, plugins.Action, error) {
	tr := c.Trc
	if tr == nil {
		// New() always populates Trc, but defensive nil-check keeps the
		// pipeline robust against direct struct construction (used in tests).
		return c.processRecordInner(ctx, rec)
	}
	ctx, span := tr.Start(ctx, "snooze.process_record")
	defer span.End()

	start := time.Now()
	out, action, err := c.processRecordInner(ctx, rec)
	if c.Reg != nil {
		c.Reg.ProcessAlertDuration.WithLabelValues("total").Observe(time.Since(start).Seconds())
	}
	span.SetAttributes(
		attribute.Int("plugins.count", len(out.Plugins)),
		attribute.String("action", action.String()),
	)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	return out, action, err
}

// processRecordInner runs the loop without the outer span; split out so the
// nil-tracer fast path can share the body.
func (c *Core) processRecordInner(ctx context.Context, rec snoozetypes.Record) (snoozetypes.Record, plugins.Action, error) {
	logger := c.Logger()
	// Tenant assertion: a record is tenant-scoped data. Without a tenant in the
	// context the driver would fail-close on the final write (ErrNoTenant) deep
	// in the pipeline, or — under platform scope — persist a tenant-less record.
	// Fail loudly up front instead so a background scanner (e.g. heartbeat) that
	// calls ProcessRecord without stamping a tenant surfaces the bug rather than
	// silently losing the alert. Platform scope is intentionally NOT accepted:
	// records always belong to exactly one tenant.
	if tenant, ok := snoozetypes.TenantFrom(ctx); !ok || tenant == "" {
		return rec, plugins.ActionAbort, fmt.Errorf("pipeline: refusing to process record without a tenant: %w", snoozetypes.ErrNoTenant)
	}
	c.stripProtected(&rec, logger)
	c.stampOKSeverityClose(ctx, &rec)
	c.stampDefaultTTL(ctx, &rec)
	for i, p := range c.processOrder {
		name := p.Name()
		rec.Plugins = append(rec.Plugins, name)

		pluginCtx := ctx
		var span trace.Span
		if c.Trc != nil {
			pluginCtx, span = c.Trc.Start(ctx, "snooze.plugin."+name+".process")
		}

		startPlugin := time.Now()
		res, perr := p.Process(pluginCtx, rec)
		if span != nil {
			span.End()
		}
		if c.Reg != nil {
			c.Reg.PluginDuration.
				WithLabelValues(name, "process").
				Observe(time.Since(startPlugin).Seconds())
		}

		if perr != nil {
			return c.abortWithException(ctx, rec, name, perr)
		}

		rec = res.Record
		switch res.Action {
		case plugins.ActionContinue:
			continue
		case plugins.ActionAbort:
			c.recordHit(name, plugins.ActionAbort)
			c.recordStatHit(ctx, rec)
			return rec, plugins.ActionAbort, nil
		case plugins.ActionAbortWrite, plugins.ActionAbortUpdate:
			// Both verdicts persist, so the suppression plugins behind this
			// one must still get a say before the write lands — see
			// plugins.Filter for why. A Filter that drops the record cancels
			// the write; anything else keeps this plugin's write semantics
			// (abort_write bumps date_epoch, abort_update does not).
			filtered, drop, fname, ferr := c.runFilters(ctx, rec, i+1)
			rec = filtered
			if ferr != nil {
				return c.abortWithException(ctx, rec, fname, ferr)
			}
			if drop {
				c.recordHit(fname, plugins.ActionAbort)
				c.recordStatHit(ctx, rec)
				return rec, plugins.ActionAbort, nil
			}
			updateTime := res.Action == plugins.ActionAbortWrite
			if err := c.writeRecord(ctx, rec, updateTime); err != nil {
				return rec, res.Action, fmt.Errorf("pipeline: write after %s: %w", res.Action, err)
			}
			c.recordHit(name, res.Action)
			c.recordStatHit(ctx, rec)
			return rec, res.Action, nil
		default:
			c.recordHit(name, res.Action)
			c.recordStatHit(ctx, rec)
			return rec, res.Action, fmt.Errorf("pipeline: plugin %q returned unknown action %d", name, res.Action)
		}
	}

	// Every plugin voted Continue: persist and return.
	if err := c.writeRecord(ctx, rec, true); err != nil {
		return rec, plugins.ActionContinue, fmt.Errorf("pipeline: final write: %w", err)
	}
	c.recordHit("__final__", plugins.ActionContinue)
	c.recordStatHit(ctx, rec)
	return rec, plugins.ActionContinue, nil
}

// runFilters gives every plugins.Filter processor at or after index start a
// say on a record whose pipeline run was cut short by an abort-and-persist
// verdict. It returns the (possibly mutated) record, whether the record must
// be dropped instead of written, and — when a filter errored — that filter's
// name and the error.
//
// The pass is deliberately narrow: it runs only on the abort-and-persist
// paths, never instead of the normal loop, so a plugin is never asked twice
// about the same record. Plugins that do not implement Filter are skipped
// (they are not suppression decisions and their side effects — notifications,
// most importantly — must stay suppressed by the abort).
func (c *Core) runFilters(ctx context.Context, rec snoozetypes.Record, start int) (snoozetypes.Record, bool, string, error) {
	if start < 0 || start >= len(c.processOrder) {
		return rec, false, "", nil
	}
	for _, p := range c.processOrder[start:] {
		f, ok := p.(plugins.Filter)
		if !ok {
			continue
		}
		name := p.Name()
		rec.Plugins = append(rec.Plugins, name)

		startFilter := time.Now()
		res, err := f.Filter(ctx, rec)
		if c.Reg != nil {
			c.Reg.PluginDuration.
				WithLabelValues(name, "filter").
				Observe(time.Since(startFilter).Seconds())
		}
		if err != nil {
			return rec, false, name, err
		}
		rec = res.Record
		if res.Action == plugins.ActionAbort {
			return rec, true, name, nil
		}
	}
	return rec, false, "", nil
}

// abortWithException is the shared failure path for a processor (or filter)
// that returned an error: the record gets an `exception` field naming the
// plugin, is written for forensics, and Abort is returned with the wrapped
// error.
func (c *Core) abortWithException(ctx context.Context, rec snoozetypes.Record, name string, perr error) (snoozetypes.Record, plugins.Action, error) {
	logger := c.Logger()
	logger.Error("pipeline: plugin returned error", "plugin", name, "err", perr)
	rec.Extra = ensureExtra(rec.Extra)
	rec.Extra["exception"] = map[string]any{
		"plugin":  name,
		"message": perr.Error(),
	}
	if werr := c.writeRecord(ctx, rec, true); werr != nil {
		logger.Error("pipeline: write after plugin error failed",
			"plugin", name, "err", werr)
	}
	c.recordHit(name, plugins.ActionAbort)
	c.recordStatHit(ctx, rec)
	return rec, plugins.ActionAbort, fmt.Errorf("pipeline: plugin %q: %w", name, perr)
}

// writeRecord upserts rec into the record collection. The updateTime flag
// matches the Python “replace_one(..., update_time=...)“ parameter and
// controls whether the storage backend stamps “updated_at“.
func (c *Core) writeRecord(ctx context.Context, rec snoozetypes.Record, updateTime bool) error {
	if c.Driver == nil {
		return nil
	}
	doc := recordToDoc(rec)
	_, err := c.Driver.Write(ctx, recordCollection, []db.Document{doc}, db.WriteOptions{
		Primary:    []string{"uid"},
		UpdateTime: updateTime,
	})
	return err
}

// recordHit bumps the AlertHit counter for the terminal plugin verdict. The
// metric is best-effort: nil registry (test mode) is silently ignored.
func (c *Core) recordHit(plugin string, action plugins.Action) {
	if c.Reg == nil {
		return
	}
	c.Reg.AlertHit.WithLabelValues(plugin, action.String()).Inc()
}

// recordStatHit bumps the persisted alert_hit counter for a terminal record,
// labelled by its final source/severity/environment/host. Bucketed by the
// alert's own date_epoch so the dashboard groups by occurrence time, not
// processing time. No-ops when metrics are disabled (see plugins.RecordStat).
func (c *Core) recordStatHit(ctx context.Context, rec snoozetypes.Record) {
	plugins.RecordStat(ctx, c, rec.DateEpoch, "alert_hit", map[string]string{
		"source":      rec.Source,
		"severity":    rec.Severity,
		"environment": rec.Environment,
		"host":        rec.Host,
	}, 1)
}

// stampOKSeverityClose closes rec when its severity matches the configured
// general.ok_severities list (default ["ok", "success"]), documented as "the
// severities that automatically close the aggregate upon entering the
// system". Snooze 1.x's Python core enforced this centrally for every
// record; the Go port loaded and documented the field but nothing ever
// consumed it, so generic inputs (syslog, snmptrap, a raw record POST) that
// sent severity "ok" never closed anything. This restores that central
// enforcement.
//
// If rec.State is already set — e.g. a webhook receiver plugin (alertmanager,
// cloudwatch, datadog, azuremonitor) stamped its own provider-status mapping
// — the input is authoritative and this stamp does nothing: it only fills an
// empty State, so those receivers are unaffected.
func (c *Core) stampOKSeverityClose(ctx context.Context, rec *snoozetypes.Record) {
	if rec.State != "" {
		return
	}
	severity := strings.ToLower(strings.TrimSpace(rec.Severity))
	if severity == "" {
		return
	}

	// Prefer the live runtime cache so an operator who edits
	// general.ok_severities in the UI sees the new value on the next alert
	// without restarting the server.
	if c.Settings != nil {
		if gen, err := c.Settings.General(ctx); err == nil {
			if severityInList(severity, gen.OKSeverities) {
				rec.State = "close"
			}
			return
		}
	}
	// Fallback to the file-config baseline (e.g. tests construct a Core
	// without a RuntimeSettings).
	if c.Cfg != nil && severityInList(severity, c.Cfg.General.OKSeverities) {
		rec.State = "close"
	}
}

// severityInList reports whether severity (already case-folded/trimmed)
// appears in list. The config lists are normalized to lowercase at load
// time (schema.General.Normalize), so no further folding is needed here.
func severityInList(severity string, list []string) bool {
	for _, s := range list {
		if s == severity {
			return true
		}
	}
	return false
}

// stampDefaultTTL fills in rec.TTL with the configured record-ttl default
// when the caller did not set one. Mirrors src/snooze/core.py:161 from
// Snooze 1.x: every fresh alert carries a `ttl` (seconds-from-date_epoch
// expiry) so the housekeeper's cleanup_timeout job can prune it. Without
// this, records ingested without an explicit TTL persist forever and the
// "Shelved" tab's `NOT EXISTS ttl` predicate matches every alert (since
// the recordToDoc projector elides TTL=0).
//
// A negative TTL means the operator deliberately shelved the alert
// (cleanup_timeout's $match: ttl >= 0 spares those), so we leave it
// alone. A positive TTL set by the caller (e.g. tests, integrations
// posting a custom expiry) is also respected.
// stripProtected drops protected fields (internal/protected) an inbound alert
// carries before any plugin sees the record.
//
// Protected fields are owned by a dedicated, schema-validating, separately
// permissioned endpoint; an alert payload claiming to carry one is either a
// mistake or an attempt to forge it. Ingestion strips rather than rejects: an
// alert is a signal about production, and refusing it outright would let a
// sender that guesses a protected name silently break its own alerting.
//
// A stored value on an EXISTING record is unaffected — the pipeline's final
// write is a merge, and a key the incoming document no longer mentions is
// left alone. So a re-fire of an already-analysed alert keeps its analysis.
func (c *Core) stripProtected(rec *snoozetypes.Record, logger *slog.Logger) {
	removed := protected.Strip(rec.Extra)
	if len(removed) == 0 {
		return
	}
	if logger != nil {
		logger.Warn("pipeline: dropped protected field(s) from inbound alert",
			"fields", removed, "host", rec.Host, "source", rec.Source)
	}
}

func (c *Core) stampDefaultTTL(ctx context.Context, rec *snoozetypes.Record) {
	if rec.TTL != 0 {
		return
	}
	// Prefer the live runtime cache so an operator who edits
	// housekeeping.record_ttl in the UI sees the new value on the next
	// alert without restarting the server.
	if c.Settings != nil {
		if hk, err := c.Settings.Housekeeper(ctx); err == nil && hk.RecordTTL.AsDuration() > 0 {
			rec.TTL = int64(hk.RecordTTL.AsDuration().Seconds())
			return
		}
	}
	// Fallback to the file-config baseline (e.g. tests construct a Core
	// without a RuntimeSettings).
	if c.Cfg != nil && c.Cfg.Housekeeper.RecordTTL.AsDuration() > 0 {
		rec.TTL = int64(c.Cfg.Housekeeper.RecordTTL.AsDuration().Seconds())
	}
}

// recordToDoc projects the typed Record into the loose Document the driver
// layer consumes. Empty fields are elided to keep the on-disk shape compact.
func recordToDoc(rec snoozetypes.Record) db.Document {
	d := db.Document{}
	if rec.UID != "" {
		d["uid"] = rec.UID
	}
	if rec.Host != "" {
		d["host"] = rec.Host
	}
	if rec.Source != "" {
		d["source"] = rec.Source
	}
	if rec.Process != "" {
		d["process"] = rec.Process
	}
	if rec.Severity != "" {
		d["severity"] = rec.Severity
	}
	if rec.Message != "" {
		d["message"] = rec.Message
	}
	if !rec.Timestamp.IsZero() {
		d["timestamp"] = rec.Timestamp
	}
	if rec.DateEpoch != 0 {
		d["date_epoch"] = rec.DateEpoch
	}
	if rec.TTL != 0 {
		d["ttl"] = rec.TTL
	}
	if rec.Environment != "" {
		d["environment"] = rec.Environment
	}
	if rec.Hash != "" {
		d["hash"] = rec.Hash
	}
	if len(rec.Tags) > 0 {
		d["tags"] = rec.Tags
	}
	if len(rec.Raw) > 0 {
		d["raw"] = rec.Raw
	}
	if rec.State != "" {
		d["state"] = rec.State
	}
	if len(rec.Plugins) > 0 {
		d["plugins"] = rec.Plugins
	}
	if rec.AckUntil != 0 {
		d["ack_until"] = rec.AckUntil
	}
	if rec.EscalateAt != 0 {
		d["escalate_at"] = rec.EscalateAt
	}
	if rec.ShelveUntil != 0 {
		d["shelve_until"] = rec.ShelveUntil
	}
	if rec.EscalationCount != 0 {
		d["escalation_count"] = rec.EscalationCount
	}
	if rec.EscalatedAt != 0 {
		d["escalated_at"] = rec.EscalatedAt
	}
	if rec.EscalationReason != "" {
		d["escalation_reason"] = rec.EscalationReason
	}
	if rec.EscalationActor != "" {
		d["escalation_actor"] = rec.EscalationActor
	}
	for k, v := range rec.Extra {
		// Extra fields override the typed ones only if the typed value was
		// not set above.
		if _, exists := d[k]; !exists {
			d[k] = v
		}
	}
	return d
}

func ensureExtra(extra map[string]any) map[string]any {
	if extra == nil {
		return map[string]any{}
	}
	return extra
}
