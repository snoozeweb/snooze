package core

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/plugins"
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
	c.stampDefaultTTL(ctx, &rec)
	for _, p := range c.processOrder {
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
			logger.Error("pipeline: plugin returned error",
				"plugin", name, "err", perr)
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

		rec = res.Record
		switch res.Action {
		case plugins.ActionContinue:
			continue
		case plugins.ActionAbort:
			c.recordHit(name, plugins.ActionAbort)
			c.recordStatHit(ctx, rec)
			return rec, plugins.ActionAbort, nil
		case plugins.ActionAbortWrite:
			if err := c.writeRecord(ctx, rec, true); err != nil {
				return rec, plugins.ActionAbortWrite, fmt.Errorf("pipeline: write after abort_write: %w", err)
			}
			c.recordHit(name, plugins.ActionAbortWrite)
			c.recordStatHit(ctx, rec)
			return rec, plugins.ActionAbortWrite, nil
		case plugins.ActionAbortUpdate:
			if err := c.writeRecord(ctx, rec, false); err != nil {
				return rec, plugins.ActionAbortUpdate, fmt.Errorf("pipeline: write after abort_update: %w", err)
			}
			c.recordHit(name, plugins.ActionAbortUpdate)
			c.recordStatHit(ctx, rec)
			return rec, plugins.ActionAbortUpdate, nil
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
