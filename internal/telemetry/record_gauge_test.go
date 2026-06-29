package telemetry

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/stretchr/testify/require"
)

// stubDriver embeds db.Driver so it satisfies the full interface; only the
// methods the RecordGaugeCollector exercises are overridden. Any unexpected
// call panics with a nil-method dispatch, surfacing accidental use.
type stubDriver struct {
	db.Driver

	mu sync.Mutex
	// stateCounts maps a record state to the count returned for a Search whose
	// condition targets that state. The "open" bucket is keyed by "open".
	stateCounts map[string]int
	searchErr   error
	// recordSearches counts Search calls against the "record" collection only
	// (the per-state count path); the "tenant" enumeration query is excluded.
	recordSearches int

	// block, when non-nil, is closed when Search is entered; Search then blocks
	// until ctx is cancelled (timeout test).
	block chan struct{}
}

func (s *stubDriver) Search(ctx context.Context, collection string, cond condition.Cond, _ db.Page) ([]db.Document, int, error) {
	if collection == "record" {
		s.mu.Lock()
		s.recordSearches++
		s.mu.Unlock()
	}

	if s.block != nil {
		// Signal entry once, then wait for cancellation.
		select {
		case <-s.block:
		default:
			close(s.block)
		}
		<-ctx.Done()
		return nil, 0, ctx.Err()
	}

	if s.searchErr != nil {
		return nil, 0, s.searchErr
	}
	// The tenant-enumeration query hits the "tenant" collection: report no
	// tenants so Collect takes the single-tenant fallback path.
	if collection == "tenant" {
		return nil, 0, nil
	}
	st := stateFromCond(cond)
	return nil, s.stateCounts[st], nil
}

func (s *stubDriver) recordSearchCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.recordSearches
}

// stateFromCond extracts the state label a record-count query is filtering on.
// The open bucket is an OR(state=open, state=""); everything else is a single
// Equals(state, X).
func stateFromCond(cond condition.Cond) string {
	if cond.Op == condition.OpOr {
		return "open"
	}
	if cond.Op == condition.OpEq && cond.Field == "state" {
		if v, ok := cond.Value.(string); ok {
			return v
		}
	}
	return ""
}

// aggStubDriver also implements db.RecordAggregator so Collect must use the
// fast path and never call Search on the record collection.
type aggStubDriver struct {
	stubDriver
	byState     map[string]int64
	statsCalled bool
}

func (a *aggStubDriver) RecordStats(_ context.Context, _, _ time.Time, _ int64) (db.RecordStatsBuckets, error) {
	a.mu.Lock()
	a.statsCalled = true
	a.mu.Unlock()
	return db.RecordStatsBuckets{ByState: a.byState}, nil
}

func (a *aggStubDriver) wasStatsCalled() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.statsCalled
}

// gathered returns the snooze_records value for a given state label, and
// whether the sample was present.
func gathered(t *testing.T, reg *prometheus.Registry, state string) (float64, bool) {
	t.Helper()
	mfs, err := reg.Gather()
	require.NoError(t, err)
	for _, mf := range mfs {
		if mf.GetName() != "snooze_records" {
			continue
		}
		for _, m := range mf.GetMetric() {
			for _, lp := range m.GetLabel() {
				if lp.GetName() == "state" && lp.GetValue() == state {
					return m.GetGauge().GetValue(), true
				}
			}
		}
	}
	return 0, false
}

func TestRecordGaugeCollector_Collect(t *testing.T) {
	t.Parallel()
	drv := &stubDriver{stateCounts: map[string]int{"open": 2, "ack": 1, "close": 1}}
	reg := prometheus.NewRegistry()
	r := NewRegistry(nil)
	r.RegisterRecordGauge(reg, drv, slog.New(slog.NewTextHandler(io.Discard, nil)))

	v, ok := gathered(t, reg, "open")
	require.True(t, ok, "open sample present")
	require.InDelta(t, 2.0, v, 0)

	v, ok = gathered(t, reg, "ack")
	require.True(t, ok, "ack sample present")
	require.InDelta(t, 1.0, v, 0)

	v, ok = gathered(t, reg, "close")
	require.True(t, ok, "close sample present")
	require.InDelta(t, 1.0, v, 0)
}

func TestRecordGaugeCollector_DBError(t *testing.T) {
	t.Parallel()
	drv := &stubDriver{searchErr: errors.New("boom")}
	reg := prometheus.NewRegistry()
	r := NewRegistry(nil)
	r.RegisterRecordGauge(reg, drv, slog.New(slog.NewTextHandler(io.Discard, nil)))

	mfs, err := reg.Gather()
	require.NoError(t, err)
	for _, mf := range mfs {
		if mf.GetName() == "snooze_records" {
			require.Empty(t, mf.GetMetric(), "no samples emitted on DB error")
		}
	}
}

func TestRecordGaugeCollector_UsesAggregatorFastPath(t *testing.T) {
	t.Parallel()
	drv := &aggStubDriver{byState: map[string]int64{"open": 5, "ack": 2}}
	reg := prometheus.NewRegistry()
	r := NewRegistry(nil)
	r.RegisterRecordGauge(reg, drv, slog.New(slog.NewTextHandler(io.Discard, nil)))

	v, ok := gathered(t, reg, "open")
	require.True(t, ok)
	require.InDelta(t, 5.0, v, 0)
	v, ok = gathered(t, reg, "ack")
	require.True(t, ok)
	require.InDelta(t, 2.0, v, 0)

	require.True(t, drv.wasStatsCalled(), "RecordStats must be called")
	// The per-state record Search must NOT run when the aggregator answers; the
	// only Search permitted is the (excluded) tenant-enumeration query.
	require.Zero(t, drv.recordSearchCount(), "Search must not be used for record counts")
}

func TestRecordGaugeCollector_Timeout(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping timeout test in -short mode")
	}
	drv := &stubDriver{block: make(chan struct{})}
	c := &RecordGaugeCollector{
		desc:    recordGaugeDesc,
		drv:     drv,
		lg:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		timeout: 200 * time.Millisecond,
	}

	done := make(chan struct{})
	go func() {
		ch := make(chan prometheus.Metric, 16)
		c.Collect(ch)
		close(done)
	}()

	select {
	case <-done:
		// Returned within a reasonable bound of the 200ms timeout.
	case <-time.After(5 * time.Second):
		t.Fatal("Collect hung past the scrape timeout")
	}
}

// Compile-time assertions that the stubs satisfy the interfaces they claim.
var (
	_ db.Driver           = (*stubDriver)(nil)
	_ db.RecordAggregator = (*aggStubDriver)(nil)
)
