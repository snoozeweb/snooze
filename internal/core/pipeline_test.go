package core

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"

	"github.com/snoozeweb/snooze/internal/config"
	"github.com/snoozeweb/snooze/internal/config/schema"
	"github.com/snoozeweb/snooze/internal/db/asyncwriter"
	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/internal/telemetry"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// fakeProcessor is a stub Processor whose Process method returns a configured
// Result + error. Name is the registry key.
type fakeProcessor struct {
	name   string
	result plugins.Result
	err    error
	calls  int
	// recvRec is what Process saw on its most recent call.
	recvRec snoozetypes.Record
}

func (f *fakeProcessor) Name() string                                 { return f.name }
func (f *fakeProcessor) Metadata() plugins.Metadata                   { return plugins.Metadata{Name: f.name} }
func (f *fakeProcessor) PostInit(context.Context, plugins.Host) error { return nil }
func (f *fakeProcessor) Reload(context.Context) error                 { return nil }
func (f *fakeProcessor) Process(_ context.Context, rec snoozetypes.Record) (plugins.Result, error) {
	f.calls++
	f.recvRec = rec
	if f.err != nil {
		return plugins.Result{}, f.err
	}
	res := f.result
	if res.Record.UID == "" {
		res.Record = rec
	}
	return res, nil
}

// pctx returns a context scoped to the default tenant. ProcessRecord now
// requires a tenant (records are tenant-scoped data), so every pipeline test
// that drives a record through must carry one — mirroring the real ingest path,
// which always stamps a tenant before reaching the pipeline.
func pctx() context.Context {
	return snoozetypes.WithTenant(context.Background(), snoozetypes.DefaultTenant)
}

func newPipelineCore(t *testing.T, procs ...plugins.Processor) (*Core, *fakeDB) {
	t.Helper()
	drv := newFakeDB()
	reg := telemetry.NewRegistry(prometheus.NewRegistry())
	c := &Core{
		Driver:  drv,
		Reg:     reg,
		Trc:     otel.Tracer("test"),
		Loggers: &telemetry.Loggers{Snooze: slog.New(slog.NewTextHandler(io.Discard, nil))},
	}
	c.processOrder = procs
	return c, drv
}

// TestProcessRecord_RejectsNakedContext is the regression test for the pipeline
// tenant assertion: ProcessRecord must fail loudly (clear error, no persisted
// record) when the caller supplies a context with no tenant. Background scanners
// such as heartbeat that call ProcessRecord without stamping a tenant would
// otherwise silently lose the alert (or, worse, write a tenant-less record).
func TestProcessRecord_RejectsNakedContext(t *testing.T) {
	t.Parallel()
	p1 := &fakeProcessor{name: "rule", result: plugins.Result{Action: plugins.ActionContinue}}
	c, drv := newPipelineCore(t, p1)

	_, _, err := c.ProcessRecord(context.Background(), snoozetypes.Record{UID: "uid-naked"})
	require.Error(t, err, "ProcessRecord must reject a context with no tenant")
	require.ErrorIs(t, err, snoozetypes.ErrNoTenant)
	require.Equal(t, 0, p1.calls, "no plugin should run without a tenant")
	require.Equal(t, 0, drv.writeCount(recordCollection),
		"a tenant-less record must never be persisted")
}

func TestProcessRecord_AllContinue_WritesFinal(t *testing.T) {
	t.Parallel()
	p1 := &fakeProcessor{name: "rule", result: plugins.Result{Action: plugins.ActionContinue}}
	p2 := &fakeProcessor{name: "snooze", result: plugins.Result{Action: plugins.ActionContinue}}
	c, drv := newPipelineCore(t, p1, p2)

	rec := snoozetypes.Record{UID: "uid-1", Message: "hello"}
	out, action, err := c.ProcessRecord(pctx(), rec)
	require.NoError(t, err)
	require.Equal(t, plugins.ActionContinue, action)
	require.Equal(t, []string{"rule", "snooze"}, out.Plugins)
	require.Equal(t, 1, drv.writeCount(recordCollection))
	require.Equal(t, 1, p1.calls)
	require.Equal(t, 1, p2.calls)
}

func TestProcessRecord_Abort_DoesNotWrite(t *testing.T) {
	t.Parallel()
	p1 := &fakeProcessor{name: "rule", result: plugins.Result{Action: plugins.ActionAbort}}
	p2 := &fakeProcessor{name: "snooze", result: plugins.Result{Action: plugins.ActionContinue}}
	c, drv := newPipelineCore(t, p1, p2)

	rec := snoozetypes.Record{UID: "uid-2"}
	out, action, err := c.ProcessRecord(pctx(), rec)
	require.NoError(t, err)
	require.Equal(t, plugins.ActionAbort, action)
	require.Equal(t, []string{"rule"}, out.Plugins)
	require.Equal(t, 0, p2.calls, "second processor must not run after abort")
	require.Equal(t, 0, drv.writeCount(recordCollection), "abort must not persist")
}

func TestProcessRecord_AbortWrite_Persists(t *testing.T) {
	t.Parallel()
	p1 := &fakeProcessor{name: "rule", result: plugins.Result{Action: plugins.ActionAbortWrite}}
	p2 := &fakeProcessor{name: "snooze", result: plugins.Result{Action: plugins.ActionContinue}}
	c, drv := newPipelineCore(t, p1, p2)

	rec := snoozetypes.Record{UID: "uid-3"}
	out, action, err := c.ProcessRecord(pctx(), rec)
	require.NoError(t, err)
	require.Equal(t, plugins.ActionAbortWrite, action)
	require.Equal(t, []string{"rule"}, out.Plugins)
	require.Equal(t, 0, p2.calls)
	require.Equal(t, 1, drv.writeCount(recordCollection))
}

func TestProcessRecord_AbortUpdate_PersistsWithoutTimestamp(t *testing.T) {
	t.Parallel()
	p1 := &fakeProcessor{name: "rule", result: plugins.Result{Action: plugins.ActionAbortUpdate}}
	c, drv := newPipelineCore(t, p1)

	_, action, err := c.ProcessRecord(pctx(), snoozetypes.Record{UID: "uid-4"})
	require.NoError(t, err)
	require.Equal(t, plugins.ActionAbortUpdate, action)
	require.Equal(t, 1, drv.writeCount(recordCollection))
}

func TestProcessRecord_PluginError_AttachesExceptionField(t *testing.T) {
	t.Parallel()
	p1 := &fakeProcessor{name: "rule", err: errors.New("dropped")}
	c, drv := newPipelineCore(t, p1)

	out, action, err := c.ProcessRecord(pctx(), snoozetypes.Record{UID: "uid-5"})
	require.Error(t, err)
	require.Equal(t, plugins.ActionAbort, action)
	require.NotNil(t, out.Extra)
	excField, ok := out.Extra["exception"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "rule", excField["plugin"])
	require.Equal(t, "dropped", excField["message"])
	require.Equal(t, 1, drv.writeCount(recordCollection),
		"plugin errors persist for forensics")
}

func TestProcessRecord_RecordMutationsFlowForward(t *testing.T) {
	t.Parallel()
	p1 := &fakeProcessor{
		name: "rule",
		result: plugins.Result{
			Action: plugins.ActionContinue,
			Record: snoozetypes.Record{UID: "uid-6", Message: "mutated"},
		},
	}
	p2 := &fakeProcessor{name: "snooze", result: plugins.Result{Action: plugins.ActionContinue}}
	c, _ := newPipelineCore(t, p1, p2)

	_, _, err := c.ProcessRecord(pctx(), snoozetypes.Record{UID: "uid-6"})
	require.NoError(t, err)
	require.Equal(t, "mutated", p2.recvRec.Message,
		"second plugin must see the mutated record from p1")
}

func TestProcessRecord_StampsDefaultTTL(t *testing.T) {
	// Mirrors src/snooze/core.py:161 — every fresh alert leaves the pipeline
	// with a TTL stamped from config.Housekeeper.RecordTTL so the
	// housekeeper's cleanup_timeout job has something to match against.
	t.Parallel()
	p1 := &fakeProcessor{name: "rule", result: plugins.Result{Action: plugins.ActionContinue}}
	c, _ := newPipelineCore(t, p1)
	c.Cfg = &config.Config{
		Housekeeper: schema.Housekeeper{RecordTTL: schema.Duration(48 * time.Hour)},
	}

	out, _, err := c.ProcessRecord(pctx(), snoozetypes.Record{UID: "uid-ttl"})
	require.NoError(t, err)
	require.Equal(t, int64(48*60*60), out.TTL)
	// Plugin receives the stamped TTL too, so downstream rules can react.
	require.Equal(t, int64(48*60*60), p1.recvRec.TTL)
}

func TestProcessRecord_PreservesCallerTTL(t *testing.T) {
	// An integration posting a custom TTL (positive or negative) must keep it.
	t.Parallel()
	p1 := &fakeProcessor{name: "rule", result: plugins.Result{Action: plugins.ActionContinue}}
	c, _ := newPipelineCore(t, p1)
	c.Cfg = &config.Config{
		Housekeeper: schema.Housekeeper{RecordTTL: schema.Duration(48 * time.Hour)},
	}

	// Positive: caller's TTL wins.
	out, _, err := c.ProcessRecord(pctx(), snoozetypes.Record{UID: "uid-a", TTL: 60})
	require.NoError(t, err)
	require.Equal(t, int64(60), out.TTL)

	// Negative: shelved by the operator at ingest; stamp must not overwrite.
	out, _, err = c.ProcessRecord(pctx(), snoozetypes.Record{UID: "uid-b", TTL: -1})
	require.NoError(t, err)
	require.Equal(t, int64(-1), out.TTL)
}

// TestProcessRecord_OKSeverityCloses_Default verifies the pipeline restores
// Snooze 1.x's central "ok_severities auto-close" enforcement: a record
// arriving with severity "ok" and no explicit state gets State "close" using
// the default general.ok_severities list (["ok", "success"]).
func TestProcessRecord_OKSeverityCloses_Default(t *testing.T) {
	t.Parallel()
	p1 := &fakeProcessor{name: "rule", result: plugins.Result{Action: plugins.ActionContinue}}
	c, _ := newPipelineCore(t, p1)
	c.Cfg = &config.Config{General: schema.DefaultGeneral()}

	_, _, err := c.ProcessRecord(pctx(), snoozetypes.Record{UID: "uid-ok", Severity: "ok"})
	require.NoError(t, err)
	require.Equal(t, "close", p1.recvRec.State)
}

// TestProcessRecord_OKSeverityCloses_CaseFold verifies severity matching is
// case-insensitive, since the config list is normalized to lowercase.
func TestProcessRecord_OKSeverityCloses_CaseFold(t *testing.T) {
	t.Parallel()
	p1 := &fakeProcessor{name: "rule", result: plugins.Result{Action: plugins.ActionContinue}}
	c, _ := newPipelineCore(t, p1)
	c.Cfg = &config.Config{General: schema.DefaultGeneral()}

	_, _, err := c.ProcessRecord(pctx(), snoozetypes.Record{UID: "uid-ok-upper", Severity: "OK"})
	require.NoError(t, err)
	require.Equal(t, "close", p1.recvRec.State)
}

// TestProcessRecord_NonOKSeverity_LeavesStateEmpty verifies a severity absent
// from the ok_severities list never gets auto-closed.
func TestProcessRecord_NonOKSeverity_LeavesStateEmpty(t *testing.T) {
	t.Parallel()
	p1 := &fakeProcessor{name: "rule", result: plugins.Result{Action: plugins.ActionContinue}}
	c, _ := newPipelineCore(t, p1)

	_, _, err := c.ProcessRecord(pctx(), snoozetypes.Record{UID: "uid-warn", Severity: "warning"})
	require.NoError(t, err)
	require.Equal(t, "", p1.recvRec.State)
}

// TestProcessRecord_ExplicitStateNotOverridden verifies that a record which
// already carries a State (e.g. a webhook receiver plugin's own provider
// status mapping) is authoritative: stampOKSeverityClose must not touch it,
// even when the severity is "ok".
func TestProcessRecord_ExplicitStateNotOverridden(t *testing.T) {
	t.Parallel()

	p1 := &fakeProcessor{name: "rule", result: plugins.Result{Action: plugins.ActionContinue}}
	c, _ := newPipelineCore(t, p1)
	_, _, err := c.ProcessRecord(pctx(), snoozetypes.Record{UID: "uid-open", Severity: "ok", State: "open"})
	require.NoError(t, err)
	require.Equal(t, "open", p1.recvRec.State)

	p2 := &fakeProcessor{name: "rule", result: plugins.Result{Action: plugins.ActionContinue}}
	c2, _ := newPipelineCore(t, p2)
	_, _, err = c2.ProcessRecord(pctx(), snoozetypes.Record{UID: "uid-close", Severity: "ok", State: "close"})
	require.NoError(t, err)
	require.Equal(t, "close", p2.recvRec.State)
}

// TestProcessRecord_OKSeverityCloses_CustomConfig verifies the Cfg fallback
// path: a custom general.ok_severities list replaces the default, so "ok" no
// longer auto-closes but the configured value ("recovered") does.
func TestProcessRecord_OKSeverityCloses_CustomConfig(t *testing.T) {
	t.Parallel()
	p1 := &fakeProcessor{name: "rule", result: plugins.Result{Action: plugins.ActionContinue}}
	c, _ := newPipelineCore(t, p1)
	c.Cfg = &config.Config{
		General: schema.General{OKSeverities: []string{"recovered"}},
	}

	_, _, err := c.ProcessRecord(pctx(), snoozetypes.Record{UID: "uid-ok-2", Severity: "ok"})
	require.NoError(t, err)
	require.Equal(t, "", p1.recvRec.State, "ok is no longer in the custom list")

	p2 := &fakeProcessor{name: "rule", result: plugins.Result{Action: plugins.ActionContinue}}
	c.processOrder = []plugins.Processor{p2}
	_, _, err = c.ProcessRecord(pctx(), snoozetypes.Record{UID: "uid-recovered", Severity: "recovered"})
	require.NoError(t, err)
	require.Equal(t, "close", p2.recvRec.State)
}

// TestProcessRecord_EmptySeverity_NeverCloses verifies an empty severity is
// left untouched — never matches the ok_severities list.
func TestProcessRecord_EmptySeverity_NeverCloses(t *testing.T) {
	t.Parallel()
	p1 := &fakeProcessor{name: "rule", result: plugins.Result{Action: plugins.ActionContinue}}
	c, _ := newPipelineCore(t, p1)

	_, _, err := c.ProcessRecord(pctx(), snoozetypes.Record{UID: "uid-empty-sev"})
	require.NoError(t, err)
	require.Equal(t, "", p1.recvRec.State)
}

func TestProcessRecord_BumpsAlertHitCounter(t *testing.T) {
	t.Parallel()
	p1 := &fakeProcessor{name: "rule", result: plugins.Result{Action: plugins.ActionAbort}}
	c, _ := newPipelineCore(t, p1)
	_, _, err := c.ProcessRecord(pctx(), snoozetypes.Record{UID: "uid-7"})
	require.NoError(t, err)
	got := testutil.ToFloat64(c.Reg.AlertHit.WithLabelValues("rule", "abort"))
	require.InDelta(t, 1.0, got, 0.0001)
}

// TestProcessRecord_AlertHitStat verifies that ProcessRecord enqueues 4
// alert_hit increment ops (one per dim: source, severity, environment, host),
// all bucketed to the UTC-hour containing the record's date_epoch.
func TestProcessRecord_AlertHitStat(t *testing.T) {
	t.Parallel()
	drv := newFakeDB()
	reg := telemetry.NewRegistry(prometheus.NewRegistry())
	mc := asyncwriter.NewMockClock(time.Unix(0, 0))
	aw := asyncwriter.New(drv, time.Hour, mc)
	c := &Core{
		Driver: drv,
		Reg:    reg,
		Trc:    otel.Tracer("test"),
		Loggers: &telemetry.Loggers{
			Snooze: slog.New(slog.NewTextHandler(io.Discard, nil)),
		},
		Async: aw,
		Cfg: &config.Config{
			General: schema.General{MetricsEnabled: true},
		},
	}
	// Empty processOrder: falls straight through to the ActionContinue terminal.

	rec := snoozetypes.Record{
		Source:      "syslog",
		Severity:    "critical",
		Environment: "prod",
		Host:        "h1",
		DateEpoch:   1780302245,
	}
	_, _, err := c.ProcessRecord(pctx(), rec)
	require.NoError(t, err)

	require.NoError(t, c.Async.Flush(context.Background()))

	// 1780302245 truncated to the UTC hour:
	// 1780302245 / 3600 = 494528.4, floor → 494528 * 3600 = 1780300800
	const wantBucket int64 = 1780300800

	ops := drv.capturedIncrements("stats")
	require.Len(t, ops, 4, "expected one alert_hit op per dim")

	wantDims := map[string]string{
		"source":      "syslog",
		"severity":    "critical",
		"environment": "prod",
		"host":        "h1",
	}
	gotDims := map[string]string{}
	for _, ci := range ops {
		search := ci.op.Search
		metric, _ := search["metric"].(string)
		require.Equal(t, "alert_hit", metric, "unexpected metric")
		bucket, _ := search["bucket"].(int64)
		require.Equal(t, wantBucket, bucket, "wrong hour bucket")
		dim, _ := search["dim"].(string)
		key, _ := search["key"].(string)
		gotDims[dim] = key
		delta := ci.op.Deltas["value"]
		require.Equal(t, int64(1), delta, "expected delta 1 for dim %s", dim)
	}
	require.Equal(t, wantDims, gotDims)
}

// TestRecordToDoc_StampsAckUntilAndEscalateAt locks in the projector contract
// for the timed-lifecycle fields: a non-zero AckUntil/EscalateAt is emitted,
// and a zero value is elided (the on-disk shape stays compact and the
// sweep queries behave predictably).
func TestRecordToDoc_StampsAckUntilAndEscalateAt(t *testing.T) {
	t.Parallel()

	doc := recordToDoc(snoozetypes.Record{UID: "r1", AckUntil: 123, EscalateAt: 456})
	require.Equal(t, int64(123), doc["ack_until"])
	require.Equal(t, int64(456), doc["escalate_at"])

	// Zero values are elided.
	zero := recordToDoc(snoozetypes.Record{UID: "r2"})
	_, hasAck := zero["ack_until"]
	require.False(t, hasAck, "zero ack_until must be elided")
	_, hasEsc := zero["escalate_at"]
	require.False(t, hasEsc, "zero escalate_at must be elided")
}

// TestRecordToDoc_StampsEscalationContext locks in the projector contract for
// the escalation-context fields notifiers branch on. Zero values must be
// elided: a record with no escalation_count is what every notifier reads as a
// first fire, and an explicit 0 on disk would be indistinguishable but noisier.
func TestRecordToDoc_StampsEscalationContext(t *testing.T) {
	t.Parallel()

	doc := recordToDoc(snoozetypes.Record{
		UID:              "r1",
		EscalationCount:  3,
		EscalatedAt:      1700000000,
		EscalationReason: "manual",
		EscalationActor:  "alice",
	})
	require.Equal(t, 3, doc["escalation_count"])
	require.Equal(t, int64(1700000000), doc["escalated_at"])
	require.Equal(t, "manual", doc["escalation_reason"])
	require.Equal(t, "alice", doc["escalation_actor"])

	zero := recordToDoc(snoozetypes.Record{UID: "r2"})
	for _, k := range []string{"escalation_count", "escalated_at", "escalation_reason", "escalation_actor"} {
		_, has := zero[k]
		require.False(t, has, "zero %s must be elided", k)
	}
}

// TestRecordToDoc_StampsShelveUntil locks in the projector contract for the
// timed-shelve field: a non-zero ShelveUntil is emitted as int64, and a zero
// value is elided (a zero shelve_until is the legacy permanent-shelve marker
// the housekeeper's `OpGt shelve_until 0` guard deliberately never sweeps).
func TestRecordToDoc_StampsShelveUntil(t *testing.T) {
	t.Parallel()

	doc := recordToDoc(snoozetypes.Record{UID: "r1", ShelveUntil: 9999})
	require.Equal(t, int64(9999), doc["shelve_until"])

	zero := recordToDoc(snoozetypes.Record{UID: "r2"})
	_, hasShelve := zero["shelve_until"]
	require.False(t, hasShelve, "zero shelve_until must be elided")
}

// fakeFilter is a fakeProcessor that also implements plugins.Filter, so the
// pipeline consults it on the abort-and-persist paths. filterResult is the
// verdict Filter returns; Process keeps fakeProcessor's behaviour.
type fakeFilter struct {
	fakeProcessor
	filterResult plugins.Result
	filterErr    error
	filterCalls  int
	filterRec    snoozetypes.Record
}

func (f *fakeFilter) Filter(_ context.Context, rec snoozetypes.Record) (plugins.Result, error) {
	f.filterCalls++
	f.filterRec = rec
	if f.filterErr != nil {
		return plugins.Result{}, f.filterErr
	}
	res := f.filterResult
	if res.Record.UID == "" {
		res.Record = rec
	}
	return res, nil
}

// TestProcessRecord_AbortUpdate_FilterCanDrop is the regression test for the
// production bug where a repeating alert stayed open and un-snoozed for a
// whole aggregate-throttle window (24h on the live "Host and Message" rule).
// aggregaterule answers a throttled duplicate with ActionAbortUpdate, which
// persists and ends the pipeline — so the `snooze` plugin behind it never got
// to discard the record. A Filter verdict of ActionAbort must cancel the
// write entirely.
func TestProcessRecord_AbortUpdate_FilterCanDrop(t *testing.T) {
	t.Parallel()
	agg := &fakeProcessor{name: "aggregaterule", result: plugins.Result{Action: plugins.ActionAbortUpdate}}
	snz := &fakeFilter{
		fakeProcessor: fakeProcessor{name: "snooze"},
		filterResult:  plugins.Result{Action: plugins.ActionAbort},
	}
	c, drv := newPipelineCore(t, agg, snz)

	out, action, err := c.ProcessRecord(pctx(), snoozetypes.Record{UID: "uid-drop"})
	require.NoError(t, err)
	require.Equal(t, plugins.ActionAbort, action,
		"a discard snooze filter must win over an aggregation hold")
	require.Equal(t, 0, drv.writeCount(recordCollection),
		"a discarded record must not be persisted")
	require.Equal(t, 1, snz.filterCalls, "the filter must be consulted")
	require.Equal(t, 0, snz.calls, "Process must not run as well as Filter")
	require.Equal(t, []string{"aggregaterule", "snooze"}, out.Plugins,
		"the filter pass belongs in the record's plugin trail")
}

// TestProcessRecord_AbortUpdate_FilterStampsAttribution covers the non-discard
// half: a matching snooze filter answers ActionAbortWrite with `snoozed`
// stamped. The record must be persisted carrying that attribution, and — this
// is the subtle part — with the ORIGINAL abort_update write semantics, so
// date_epoch is not re-stamped and the aggregate's throttle window is not
// restarted by the suppression pass.
func TestProcessRecord_AbortUpdate_FilterStampsAttribution(t *testing.T) {
	t.Parallel()
	agg := &fakeProcessor{name: "aggregaterule", result: plugins.Result{Action: plugins.ActionAbortUpdate}}
	snz := &fakeFilter{
		fakeProcessor: fakeProcessor{name: "snooze"},
		filterResult: plugins.Result{
			Action: plugins.ActionAbortWrite,
			Record: snoozetypes.Record{UID: "uid-stamp", Extra: map[string]any{"snoozed": "Warnings"}},
		},
	}
	c, drv := newPipelineCore(t, agg, snz)

	out, action, err := c.ProcessRecord(pctx(), snoozetypes.Record{UID: "uid-stamp"})
	require.NoError(t, err)
	require.Equal(t, plugins.ActionAbortUpdate, action,
		"the aggregation verdict still decides the pipeline outcome")
	require.Equal(t, "Warnings", out.Extra["snoozed"])
	require.Equal(t, 1, drv.writeCount(recordCollection))
	require.False(t, drv.lastWriteOpts(recordCollection).UpdateTime,
		"abort_update must not bump date_epoch just because a filter ran")
}

// TestProcessRecord_AbortWrite_FilterKeepsFreshTimestamp is the mirror of the
// above for abort_write: the write must still opt into UpdateTime.
func TestProcessRecord_AbortWrite_FilterKeepsFreshTimestamp(t *testing.T) {
	t.Parallel()
	p1 := &fakeProcessor{name: "rule", result: plugins.Result{Action: plugins.ActionAbortWrite}}
	snz := &fakeFilter{
		fakeProcessor: fakeProcessor{name: "snooze"},
		filterResult:  plugins.Result{Action: plugins.ActionContinue},
	}
	c, drv := newPipelineCore(t, p1, snz)

	_, action, err := c.ProcessRecord(pctx(), snoozetypes.Record{UID: "uid-fresh"})
	require.NoError(t, err)
	require.Equal(t, plugins.ActionAbortWrite, action)
	require.Equal(t, 1, snz.filterCalls)
	require.True(t, drv.lastWriteOpts(recordCollection).UpdateTime)
}

// TestProcessRecord_Abort_SkipsFilters: ActionAbort persists nothing, so there
// is no write for a filter to have an opinion about.
func TestProcessRecord_Abort_SkipsFilters(t *testing.T) {
	t.Parallel()
	p1 := &fakeProcessor{name: "rule", result: plugins.Result{Action: plugins.ActionAbort}}
	snz := &fakeFilter{
		fakeProcessor: fakeProcessor{name: "snooze"},
		filterResult:  plugins.Result{Action: plugins.ActionContinue},
	}
	c, drv := newPipelineCore(t, p1, snz)

	_, action, err := c.ProcessRecord(pctx(), snoozetypes.Record{UID: "uid-plain-abort"})
	require.NoError(t, err)
	require.Equal(t, plugins.ActionAbort, action)
	require.Equal(t, 0, snz.filterCalls)
	require.Equal(t, 0, drv.writeCount(recordCollection))
}

// TestProcessRecord_FilterOnlyRunsBehindTheAbortingPlugin: a filter that sits
// BEFORE the plugin that aborted has already had its normal Process turn, so
// the suppression pass must not ask it a second time.
func TestProcessRecord_FilterOnlyRunsBehindTheAbortingPlugin(t *testing.T) {
	t.Parallel()
	snz := &fakeFilter{
		fakeProcessor: fakeProcessor{name: "snooze", result: plugins.Result{Action: plugins.ActionContinue}},
		filterResult:  plugins.Result{Action: plugins.ActionAbort},
	}
	agg := &fakeProcessor{name: "aggregaterule", result: plugins.Result{Action: plugins.ActionAbortUpdate}}
	c, drv := newPipelineCore(t, snz, agg)

	_, action, err := c.ProcessRecord(pctx(), snoozetypes.Record{UID: "uid-order"})
	require.NoError(t, err)
	require.Equal(t, plugins.ActionAbortUpdate, action)
	require.Equal(t, 1, snz.calls, "the filter already ran as a processor")
	require.Equal(t, 0, snz.filterCalls, "and must not be asked twice")
	require.Equal(t, 1, drv.writeCount(recordCollection))
}

// TestProcessRecord_FilterError_AttachesExceptionField: a broken filter takes
// the same forensic path as a broken processor rather than silently losing the
// suppression decision.
func TestProcessRecord_FilterError_AttachesExceptionField(t *testing.T) {
	t.Parallel()
	agg := &fakeProcessor{name: "aggregaterule", result: plugins.Result{Action: plugins.ActionAbortUpdate}}
	snz := &fakeFilter{
		fakeProcessor: fakeProcessor{name: "snooze"},
		filterErr:     errors.New("cache cold"),
	}
	c, drv := newPipelineCore(t, agg, snz)

	out, action, err := c.ProcessRecord(pctx(), snoozetypes.Record{UID: "uid-filter-err"})
	require.Error(t, err)
	require.Equal(t, plugins.ActionAbort, action)
	exc, ok := out.Extra["exception"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "snooze", exc["plugin"])
	require.Equal(t, 1, drv.writeCount(recordCollection))
}
