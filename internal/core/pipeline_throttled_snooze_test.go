package core

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"

	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/config"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/db/sqlite"
	"github.com/snoozeweb/snooze/internal/pluginimpl/aggregaterule"
	snoozeplugin "github.com/snoozeweb/snooze/internal/pluginimpl/snooze"
	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/internal/telemetry"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// newThrottleCore wires the real aggregaterule + snooze plugins over a real
// SQLite driver, with an aggregate rule carrying the day-long throttle that
// production uses.
func newThrottleCore(t *testing.T) (*Core, db.Driver, *snoozeplugin.Plugin, context.Context) {
	t.Helper()
	ctx := snoozetypes.WithTenant(context.Background(), snoozetypes.DefaultTenant)
	drv, err := sqlite.New(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "snooze.db")})
	require.NoError(t, err)
	t.Cleanup(func() { _ = drv.Close() })

	_, err = drv.Write(ctx, "aggregaterule", []db.Document{{
		"name":     "Host and Message",
		"fields":   []string{"host", "message"},
		"watch":    []string{"severity"},
		"throttle": int64(86400),
		"flapping": int64(3),
		"enabled":  true,
	}}, db.WriteOptions{})
	require.NoError(t, err)

	discard := slog.New(slog.NewTextHandler(io.Discard, nil))
	host := &csmHost{
		driver: drv,
		logger: discard,
		cfg:    config.Default(),
		metr:   telemetry.NewRegistry(prometheus.NewRegistry()),
		tracer: otel.Tracer("throttled-snooze-test"),
	}
	agg := &aggregaterule.Plugin{}
	require.NoError(t, agg.PostInit(ctx, host))
	snz := &snoozeplugin.Plugin{}
	require.NoError(t, snz.PostInit(ctx, host))

	c := &Core{
		Driver:  drv,
		Reg:     telemetry.NewRegistry(prometheus.NewRegistry()),
		Trc:     otel.Tracer("throttled-snooze-test"),
		Loggers: &telemetry.Loggers{Snooze: discard},
	}
	c.processOrder = []plugins.Processor{agg, snz}
	return c, drv, snz, ctx
}

// wafAlert is the live alert this regression is about: a warning from
// AlertManager that repeats every ~30 seconds.
func wafAlert() snoozetypes.Record {
	return snoozetypes.Record{
		Host: "-", Message: "WAF Nctrld process down on ",
		Process: "WAF alerts", Severity: "warning", Source: "AlertManager",
	}
}

// TestThrottledDuplicate_StillSnoozed reproduces the production incident the
// aggregaterule/snooze pair caused together.
//
// Live setup on snooze.egerie.eu: one aggregate rule with
// `throttle: {default: 86400}`, plus snooze filters matching the alert. An
// AlertManager alert repeating every 30s passed through once, and every
// occurrence for the next 24 hours was answered ActionAbortUpdate by
// aggregaterule's throttle branch — which persists the record AND ends the
// pipeline, so the snooze plugin sitting behind it never ran. Five WAF alerts
// therefore sat in the alerts list, open and un-snoozed with `duplicates`
// ticking up, while filters that matched them exactly did nothing.
func TestThrottledDuplicate_StillSnoozed(t *testing.T) {
	t.Parallel()
	c, drv, snz, ctx := newThrottleCore(t)

	_, err := drv.Write(ctx, "snooze", []db.Document{{
		"name":      "WAF processes",
		"condition": []any{"=", "process", "WAF alerts"},
		"enabled":   true,
	}}, db.WriteOptions{})
	require.NoError(t, err)
	require.NoError(t, snz.Reload(ctx))

	// First occurrence: no existing aggregate, so snooze runs in the ordinary
	// loop and attributes the record.
	out, action, err := c.ProcessRecord(ctx, wafAlert())
	require.NoError(t, err)
	require.Equal(t, plugins.ActionAbortWrite, action)
	require.Equal(t, "WAF processes", out.Extra["snoozed"])

	// Second occurrence, seconds later: a throttled duplicate. This is the
	// path that used to write an un-snoozed open row.
	out, action, err = c.ProcessRecord(ctx, wafAlert())
	require.NoError(t, err)
	require.Equal(t, plugins.ActionAbortUpdate, action,
		"the aggregate throttle still decides the pipeline verdict")
	require.Equal(t, "WAF processes", out.Extra["snoozed"],
		"a throttled duplicate must still carry its snooze attribution")

	stored, err := drv.GetOne(ctx, recordCollection, db.Document{"uid": out.UID})
	require.NoError(t, err)
	require.Equal(t, "WAF processes", stored["snoozed"],
		"the persisted row is what the alerts list reads")
}

// TestThrottledDuplicate_DiscardFilterAddedLater is the operator's actual
// workflow: the alert is already on the books and repeating when the discard
// filter is created. Every subsequent occurrence is a throttled duplicate, so
// before the Filter pass the filter had no effect at all for a whole day.
func TestThrottledDuplicate_DiscardFilterAddedLater(t *testing.T) {
	t.Parallel()
	c, drv, snz, ctx := newThrottleCore(t)

	// The alert lands and aggregates with no filters in play.
	_, action, err := c.ProcessRecord(ctx, wafAlert())
	require.NoError(t, err)
	require.Equal(t, plugins.ActionContinue, action)

	countRecords := func() int {
		_, n, err := drv.Search(ctx, recordCollection, condition.Cond{}, db.Page{})
		require.NoError(t, err)
		return n
	}
	require.Equal(t, 1, countRecords())

	// Operator creates the discard filter.
	_, err = drv.Write(ctx, "snooze", []db.Document{{
		"name":      "Warnings",
		"condition": []any{"=", "severity", "warning"},
		"discard":   true,
		"enabled":   true,
	}}, db.WriteOptions{})
	require.NoError(t, err)
	require.NoError(t, snz.Reload(ctx))

	// The next occurrence is throttled — and must be discarded anyway.
	_, action, err = c.ProcessRecord(ctx, wafAlert())
	require.NoError(t, err)
	require.Equal(t, plugins.ActionAbort, action,
		"a discard filter must win over the aggregate throttle hold")
	require.Equal(t, 1, countRecords(),
		"the discarded occurrence must not add or rewrite a row")
}
