package aggregaterule

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/config"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/db/asyncwriter"
	"github.com/snoozeweb/snooze/internal/db/sqlite"
	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/internal/telemetry"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// --- test scaffolding ---

type testHost struct {
	driver db.Driver
	writer *asyncwriter.Writer
	cfg    *config.Config
	logger *slog.Logger
	tracer trace.Tracer
	metr   *telemetry.Registry
	plugs  map[string]plugins.Plugin
}

func newTestHost(t *testing.T) *testHost {
	t.Helper()
	path := filepath.Join(t.TempDir(), "snooze.db")
	drv, err := sqlite.New(context.Background(), sqlite.Config{Path: path})
	require.NoError(t, err)
	t.Cleanup(func() { _ = drv.Close() })
	return &testHost{
		driver: drv,
		cfg:    config.Default(),
		logger: slog.Default(),
		tracer: otel.Tracer("aggregaterule-test"),
		metr:   telemetry.NewRegistry(nil),
		plugs:  map[string]plugins.Plugin{},
	}
}

func (h *testHost) DB() db.Driver                { return h.driver }
func (h *testHost) Bus() plugins.Bus             { return nil }
func (h *testHost) Logger() *slog.Logger         { return h.logger }
func (h *testHost) Tracer() trace.Tracer         { return h.tracer }
func (h *testHost) Metrics() *telemetry.Registry { return h.metr }
func (h *testHost) Config() *config.Config       { return h.cfg }
func (h *testHost) Plugin(name string) plugins.Plugin {
	return h.plugs[name]
}
func (h *testHost) AsyncWriter() *asyncwriter.Writer { return h.writer }

// writeRule persists an aggregate-rule definition.
func writeRule(t *testing.T, host *testHost, rule db.Document) {
	t.Helper()
	_, err := host.driver.Write(context.Background(), ruleCollection,
		[]db.Document{rule}, db.WriteOptions{Primary: []string{"name"}, UpdateTime: true})
	require.NoError(t, err)
}

// runProcess invokes the plugin and immediately persists the (possibly
// updated) record back to the `record` collection, mirroring what the real
// pipeline does on ActionContinue / AbortUpdate.
func runProcess(t *testing.T, p *Plugin, host *testHost, in snoozetypes.Record) (snoozetypes.Record, plugins.Action) {
	t.Helper()
	res, err := p.Process(context.Background(), in)
	require.NoError(t, err)
	if res.Action == plugins.ActionContinue || res.Action == plugins.ActionAbortWrite || res.Action == plugins.ActionAbortUpdate {
		doc := recordToMap(res.Record)
		// Carry the per-record fields the plugin stamped via Extra back
		// onto the document for storage.
		for k, v := range res.Record.Extra {
			if _, ok := doc[k]; !ok {
				doc[k] = v
			}
		}
		match := db.Document{}
		if h, ok := doc["hash"].(string); ok {
			match["hash"] = h
		}
		_, err := host.driver.ReplaceOne(context.Background(), recordCollection, match, doc, true)
		require.NoError(t, err)
	}
	return res.Record, res.Action
}

func freshPlugin(t *testing.T, host *testHost) *Plugin {
	t.Helper()
	p := &Plugin{meta: plugins.Metadata{Name: "aggregaterule"}}
	// Override the clock so throttle checks are deterministic.
	frozen := time.Unix(1_700_000_000, 0)
	p.clock = func() time.Time { return frozen }
	require.NoError(t, p.PostInit(context.Background(), host))
	return p
}

// recordsByAggregate lists every persisted record whose `aggregate` field
// equals name, in seq order.
func recordsByAggregate(t *testing.T, host *testHost, name string) []db.Document {
	t.Helper()
	docs, _, err := host.driver.Search(context.Background(), recordCollection,
		condition.Equals("aggregate", name), db.Page{})
	require.NoError(t, err)
	return docs
}

// --- tests ---

// TestAggregateRuleObject_Match mirrors Python's TestAggregate.test_match: a
// matching condition sets `aggregate` on the record.
func TestAggregateRuleObject_Match(t *testing.T) {
	t.Parallel()

	host := newTestHost(t)
	writeRule(t, host, db.Document{
		"name":      "Agg1",
		"condition": []any{"=", "a", "1"},
		"fields":    []string{"a", "b"},
		"throttle":  int64(15),
	})
	p := freshPlugin(t, host)

	rec := snoozetypes.Record{Extra: map[string]any{"a": "1", "b": "2"}}
	out, action := runProcess(t, p, host, rec)
	require.Equal(t, plugins.ActionContinue, action)
	require.Equal(t, "Agg1", out.Extra["aggregate"])
	require.NotEmpty(t, out.Hash)
}

// TestAggregate_Throttle ports Python's test_aggregate_throttle: same hash
// inside the window collapses; different hash flows through.
func TestAggregate_Throttle(t *testing.T) {
	t.Parallel()

	host := newTestHost(t)
	writeRule(t, host, db.Document{
		"name":      "Agg1",
		"condition": []any{"=", "a", "1"},
		"fields":    []string{"a", "b"},
		"throttle":  int64(900),
	})
	p := freshPlugin(t, host)

	// Three records matching Agg1 with same {a,b}=(1,2)
	for i := 0; i < 3; i++ {
		runProcess(t, p, host, snoozetypes.Record{Extra: map[string]any{
			"a": "1", "b": "2", "c": "x",
		}})
	}
	// One Agg1 record with different b → distinct hash
	runProcess(t, p, host, snoozetypes.Record{Extra: map[string]any{
		"a": "1", "b": "0",
	}})

	results := recordsByAggregate(t, host, "Agg1")
	require.Len(t, results, 2)
	// Find each bucket by their b value.
	byB := map[string]int64{}
	for _, r := range results {
		b, _ := r["b"].(string)
		byB[b] = toInt64(r["duplicates"], 0)
	}
	require.Equal(t, int64(3), byB["2"])
	require.Equal(t, int64(1), byB["0"])
}

// TestAggregate_NoThrottle ports Python's test_aggregate_nothrottle: with
// throttle <= 0 every duplicate still aggregates onto the same hash.
func TestAggregate_NoThrottle(t *testing.T) {
	t.Parallel()

	host := newTestHost(t)
	writeRule(t, host, db.Document{
		"name":      "Agg3",
		"condition": []any{"=", "a", "3"},
		"fields":    []string{"a", "b"},
		"throttle":  int64(0),
	})
	p := freshPlugin(t, host)

	runProcess(t, p, host, snoozetypes.Record{Extra: map[string]any{"a": "3", "b": "2", "c": "3"}})
	runProcess(t, p, host, snoozetypes.Record{Extra: map[string]any{"a": "3", "b": "2", "c": "4"}})

	results := recordsByAggregate(t, host, "Agg3")
	require.Len(t, results, 1)
	require.Equal(t, int64(2), toInt64(results[0]["duplicates"], 0))
}

// TestAggregate_WatchedFields ports test_aggregate_watchedfields: a change
// in a watched field bumps comment_count (re-escalation) but otherwise
// aggregates normally.
func TestAggregate_WatchedFields(t *testing.T) {
	t.Parallel()

	host := newTestHost(t)
	writeRule(t, host, db.Document{
		"name":      "Agg4",
		"condition": []any{"=", "a", "4"},
		"fields":    []string{"a", "b"},
		"watch":     []string{"c"},
		"throttle":  int64(900),
		"flapping":  int64(3),
	})
	p := freshPlugin(t, host)

	runProcess(t, p, host, snoozetypes.Record{Extra: map[string]any{"a": "4", "b": "2", "c": "3"}})
	runProcess(t, p, host, snoozetypes.Record{Extra: map[string]any{"a": "4", "b": "2", "c": "4"}})

	results := recordsByAggregate(t, host, "Agg4")
	require.Len(t, results, 1)
	require.Equal(t, int64(2), toInt64(results[0]["duplicates"], 0))
	require.Equal(t, int64(1), toInt64(results[0]["comment_count"], 0))
}

// TestAggregate_OK ports test_aggregate_ok: an incoming "close" against an
// open aggregate closes it.
func TestAggregate_OK(t *testing.T) {
	t.Parallel()

	host := newTestHost(t)
	writeRule(t, host, db.Document{
		"name":      "Agg1",
		"condition": []any{"=", "a", "1"},
		"fields":    []string{"a"},
		"throttle":  int64(900),
	})
	p := freshPlugin(t, host)

	runProcess(t, p, host, snoozetypes.Record{State: "open", Extra: map[string]any{"a": "1"}})
	runProcess(t, p, host, snoozetypes.Record{State: "close", Extra: map[string]any{"a": "1"}})

	results := recordsByAggregate(t, host, "Agg1")
	require.Len(t, results, 1)
	require.Equal(t, int64(2), toInt64(results[0]["duplicates"], 0))
	require.Equal(t, "close", results[0]["state"])
}

// TestAggregate_Flapping ports test_aggregate_flapping: state churn decrements
// flapping_countdown; once it hits 0 the alert is held back.
func TestAggregate_Flapping(t *testing.T) {
	t.Parallel()

	host := newTestHost(t)
	writeRule(t, host, db.Document{
		"name":      "Agg4",
		"condition": []any{"=", "a", "4"},
		"fields":    []string{"a"},
		"watch":     []string{"c"},
		"throttle":  int64(900),
		"flapping":  int64(3),
	})
	p := freshPlugin(t, host)

	records := []snoozetypes.Record{
		{State: "open", Extra: map[string]any{"a": "4", "c": "1"}},
		{State: "close", Extra: map[string]any{"a": "4", "c": "1"}},
		{State: "open", Extra: map[string]any{"a": "4", "c": "2"}},
		{State: "open", Extra: map[string]any{"a": "4", "c": "3"}},
		{State: "open", Extra: map[string]any{"a": "4", "c": "4"}},
	}
	for _, r := range records {
		runProcess(t, p, host, r)
	}

	results := recordsByAggregate(t, host, "Agg4")
	require.Len(t, results, 1)
	require.Equal(t, int64(5), toInt64(results[0]["duplicates"], 0))
	require.LessOrEqual(t, toInt64(results[0]["flapping_countdown"], 99), int64(0))
}

// TestAggregate_Burst ports test_aggregate_burst: two records with the same
// hash in flight. The second observes the persisted first and aggregates.
func TestAggregate_Burst(t *testing.T) {
	t.Parallel()

	host := newTestHost(t)
	writeRule(t, host, db.Document{
		"name":      "Agg1",
		"condition": []any{"=", "a", float64(1)},
		"fields":    []string{"a"},
		"throttle":  int64(900),
	})
	p := freshPlugin(t, host)

	_, action1 := runProcess(t, p, host, snoozetypes.Record{Extra: map[string]any{"a": float64(1)}})
	require.Equal(t, plugins.ActionContinue, action1)

	// Second time the same record arrives: existing aggregate is found and
	// throttle drops the duplicate.
	_, action2 := runProcess(t, p, host, snoozetypes.Record{Extra: map[string]any{"a": float64(1)}})
	require.Equal(t, plugins.ActionAbortUpdate, action2)
}

// TestAggregate_CarriesForwardInjectedResponse mirrors Snooze 1.x's full-record
// merge (plugin.py:73 `dict(aggregate.items() + record.items())`): on a
// duplicate match, a `response_<action>` field a previous notification injected
// onto the stored aggregate must ride forward onto the in-memory record. The
// notification/webhook reads it to thread the Teams follow-up under the recorded
// message; the incoming alert never carries it, so without carry-forward it is
// invisible to the pipeline.
func TestAggregate_CarriesForwardInjectedResponse(t *testing.T) {
	t.Parallel()

	host := newTestHost(t)
	writeRule(t, host, db.Document{
		"name":      "Agg1",
		"condition": []any{"=", "a", "1"},
		"fields":    []string{"a"},
		"throttle":  int64(900),
	})
	p := freshPlugin(t, host)

	// First fire creates the aggregate row.
	out1, _ := runProcess(t, p, host, snoozetypes.Record{Extra: map[string]any{"a": "1"}})
	require.NotEmpty(t, out1.Hash)

	// A prior notification injects a response onto the stored row, keyed by
	// hash — exactly what the notification plugin's inject closure does.
	injected := map[string]any{"message_ids": map[string]any{"teams/x/channels/y": "1700000000001"}}
	_, err := host.driver.SetFields(context.Background(), recordCollection,
		db.Document{"response_Teams": injected},
		condition.Equals("hash", out1.Hash))
	require.NoError(t, err)

	// Duplicate fire: the in-memory record must carry response_Teams forward
	// from the stored aggregate.
	out2, _ := runProcess(t, p, host, snoozetypes.Record{Extra: map[string]any{"a": "1"}})
	require.NotNil(t, out2.Extra["response_Teams"],
		"response_Teams must be carried forward from the existing aggregate onto the record")
}

// TestPostInit_SeedsDefault verifies that PostInit writes the `_default`
// aggregate rule with fingerprint [host, source, message] when the
// `aggregaterule` collection is empty.
func TestPostInit_SeedsDefault(t *testing.T) {
	t.Parallel()
	host := newTestHost(t)
	_ = freshPlugin(t, host)

	docs, _, err := host.driver.Search(context.Background(), ruleCollection,
		condition.Equals("name", "_default"), db.Page{})
	require.NoError(t, err)
	require.Len(t, docs, 1)
	require.Equal(t, "_default", docs[0]["name"])
	fields := toStringSlice(docs[0]["fields"])
	require.ElementsMatch(t, []string{"host", "source", "message"}, fields)
}

// TestPostInit_DoesNotReseed verifies the seed is idempotent: an existing
// rule set is left untouched.
func TestPostInit_DoesNotReseed(t *testing.T) {
	t.Parallel()
	host := newTestHost(t)
	writeRule(t, host, db.Document{
		"name":      "Custom",
		"condition": []any{},
		"fields":    []string{"x"},
		"throttle":  int64(30),
	})
	_ = freshPlugin(t, host)

	all, _, err := host.driver.Search(context.Background(), ruleCollection,
		condition.Cond{}, db.Page{})
	require.NoError(t, err)
	require.Len(t, all, 1)
	require.Equal(t, "Custom", all[0]["name"])
}

// TestAsyncWriter_Increments verifies the plugin queues a `duplicates`
// increment via the host's async writer for an already-closed record.
func TestAsyncWriter_Increments(t *testing.T) {
	t.Parallel()

	host := newTestHost(t)
	writeRule(t, host, db.Document{
		"name":      "Agg1",
		"condition": []any{"=", "a", "1"},
		"fields":    []string{"a"},
		"throttle":  int64(900),
	})

	clock := asyncwriter.NewMockClock(time.Unix(0, 0))
	w := asyncwriter.New(host.driver, 10*time.Millisecond, clock, asyncwriter.WithUpsert(false))
	host.writer = w

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()

	p := freshPlugin(t, host)

	// Seed an already-closed aggregate.
	runProcess(t, p, host, snoozetypes.Record{State: "open", Extra: map[string]any{"a": "1"}})
	runProcess(t, p, host, snoozetypes.Record{State: "close", Extra: map[string]any{"a": "1"}})
	// Now a second close → should be ActionAbortUpdate and bump duplicates
	// via the async writer.
	_, action := runProcess(t, p, host, snoozetypes.Record{State: "close", Extra: map[string]any{"a": "1"}})
	require.Equal(t, plugins.ActionAbortUpdate, action)

	// Trigger a flush.
	require.Eventually(t, func() bool {
		// We can't peek at the writer's bucket from outside; instead, advance
		// the clock and wait for the bulk increment to land in the DB.
		clock.Advance(10 * time.Millisecond)
		results := recordsByAggregate(t, host, "Agg1")
		if len(results) != 1 {
			return false
		}
		return toInt64(results[0]["duplicates"], 0) >= 3
	}, 2*time.Second, 20*time.Millisecond)

	cancel()
	<-done
}
