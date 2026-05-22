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

func TestProcessRecord_AllContinue_WritesFinal(t *testing.T) {
	t.Parallel()
	p1 := &fakeProcessor{name: "rule", result: plugins.Result{Action: plugins.ActionContinue}}
	p2 := &fakeProcessor{name: "snooze", result: plugins.Result{Action: plugins.ActionContinue}}
	c, drv := newPipelineCore(t, p1, p2)

	rec := snoozetypes.Record{UID: "uid-1", Message: "hello"}
	out, action, err := c.ProcessRecord(context.Background(), rec)
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
	out, action, err := c.ProcessRecord(context.Background(), rec)
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
	out, action, err := c.ProcessRecord(context.Background(), rec)
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

	_, action, err := c.ProcessRecord(context.Background(), snoozetypes.Record{UID: "uid-4"})
	require.NoError(t, err)
	require.Equal(t, plugins.ActionAbortUpdate, action)
	require.Equal(t, 1, drv.writeCount(recordCollection))
}

func TestProcessRecord_PluginError_AttachesExceptionField(t *testing.T) {
	t.Parallel()
	p1 := &fakeProcessor{name: "rule", err: errors.New("dropped")}
	c, drv := newPipelineCore(t, p1)

	out, action, err := c.ProcessRecord(context.Background(), snoozetypes.Record{UID: "uid-5"})
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

	_, _, err := c.ProcessRecord(context.Background(), snoozetypes.Record{UID: "uid-6"})
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

	out, _, err := c.ProcessRecord(context.Background(), snoozetypes.Record{UID: "uid-ttl"})
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
	out, _, err := c.ProcessRecord(context.Background(), snoozetypes.Record{UID: "uid-a", TTL: 60})
	require.NoError(t, err)
	require.Equal(t, int64(60), out.TTL)

	// Negative: shelved by the operator at ingest; stamp must not overwrite.
	out, _, err = c.ProcessRecord(context.Background(), snoozetypes.Record{UID: "uid-b", TTL: -1})
	require.NoError(t, err)
	require.Equal(t, int64(-1), out.TTL)
}

func TestProcessRecord_BumpsAlertHitCounter(t *testing.T) {
	t.Parallel()
	p1 := &fakeProcessor{name: "rule", result: plugins.Result{Action: plugins.ActionAbort}}
	c, _ := newPipelineCore(t, p1)
	_, _, err := c.ProcessRecord(context.Background(), snoozetypes.Record{UID: "uid-7"})
	require.NoError(t, err)
	got := testutil.ToFloat64(c.Reg.AlertHit.WithLabelValues("rule", "abort"))
	require.InDelta(t, 1.0, got, 0.0001)
}
