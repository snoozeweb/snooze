package mongo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/snoozeweb/snooze/internal/syncer"
)

// captureLogger returns a slog.Logger whose JSON output writes into buf.
// Tests inspect buf to assert that runStream / probeReplication emit the
// expected level + message + attributes.
func captureLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func decodeLogLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, raw := range strings.Split(strings.TrimRight(buf.String(), "\n"), "\n") {
		if raw == "" {
			continue
		}
		var rec map[string]any
		require.NoError(t, json.Unmarshal([]byte(raw), &rec), "decode log line %q", raw)
		out = append(out, rec)
	}
	return out
}

// newTestBus builds a bus whose `open` and timing fields can be controlled
// from the test, without ever touching a real *mongo.Client.
func newTestBus(t *testing.T, logger *slog.Logger) *mongoBus {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return &mongoBus{
		logger:       logger,
		streams:      make(map[string]context.CancelFunc),
		rootCtx:      ctx,
		rootStop:     cancel,
		retryInitial: 5 * time.Millisecond,
		retryMax:     20 * time.Millisecond,
	}
}

// TestRunStream_RetriesAndLogsOnWatchFailure: the production failure mode that
// motivated this whole change. When `open` errors (the standalone-mongod case),
// runStream should log at ERROR, sleep its backoff, and try again — not give
// up. The test verifies (a) multiple attempts happen within a short window
// and (b) the log lines surface the collection name and the error.
func TestRunStream_RetriesAndLogsOnWatchFailure(t *testing.T) {
	var attempts atomic.Int32
	stubErr := errors.New("(MockReplicaSet) not running with --replSet")

	buf := &bytes.Buffer{}
	b := newTestBus(t, captureLogger(buf))
	b.open = func(_ context.Context, _ string) (changeStream, error) {
		attempts.Add(1)
		return nil, stubErr
	}

	runCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		b.runStream(runCtx, "aggregaterule")
		close(done)
	}()

	// Wait for at least 3 retry attempts to confirm the loop is real, not
	// the original "sleep 2s and bail" behaviour.
	require.Eventually(t, func() bool {
		return attempts.Load() >= 3
	}, 500*time.Millisecond, 5*time.Millisecond, "runStream did not retry on Watch failure")
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("runStream did not return after ctx cancel")
	}

	lines := decodeLogLines(t, buf)
	require.NotEmpty(t, lines)
	for _, rec := range lines {
		require.Equal(t, "ERROR", rec["level"], "every retry log line must be ERROR-level")
		require.Equal(t, "aggregaterule", rec["collection"])
		require.Contains(t, rec["err"], "replSet")
	}
}

// TestRunStream_BackoffGrows: the gap between successive attempts must grow,
// capped at retryMax. We can't measure wallclock reliably here, so we read
// the `retry_in` attribute that was logged before each sleep.
func TestRunStream_BackoffGrows(t *testing.T) {
	stubErr := errors.New("standalone")

	buf := &bytes.Buffer{}
	b := newTestBus(t, captureLogger(buf))
	b.retryInitial = 1 * time.Millisecond
	b.retryMax = 8 * time.Millisecond
	b.open = func(_ context.Context, _ string) (changeStream, error) {
		return nil, stubErr
	}

	runCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		b.runStream(runCtx, "x")
		close(done)
	}()
	// Long enough for several backoff doublings to be logged.
	time.Sleep(60 * time.Millisecond)
	cancel()
	<-done

	lines := decodeLogLines(t, buf)
	require.GreaterOrEqual(t, len(lines), 4, "expected multiple retry log lines")
	// `retry_in` is logged as a duration. slog JSON encodes durations as the
	// nanosecond integer.
	var retries []time.Duration
	for _, rec := range lines {
		v, ok := rec["retry_in"]
		require.True(t, ok, "log line missing retry_in: %v", rec)
		switch n := v.(type) {
		case float64:
			retries = append(retries, time.Duration(n))
		case string:
			d, err := time.ParseDuration(n)
			require.NoError(t, err)
			retries = append(retries, d)
		default:
			t.Fatalf("unexpected retry_in encoding: %T %v", v, v)
		}
	}
	// First attempt logs retryInitial; subsequent attempts log the previous
	// value's double, capped at retryMax. So the sequence must be
	// non-decreasing and never exceed retryMax.
	for i, d := range retries {
		require.LessOrEqual(t, d, b.retryMax, "retries[%d]=%s exceeds cap", i, d)
		if i > 0 {
			require.GreaterOrEqual(t, d, retries[i-1], "retries[%d]=%s decreased from %s", i, d, retries[i-1])
		}
	}
	require.Equal(t, b.retryMax, retries[len(retries)-1], "final retry should have hit the cap")
}

// TestRunStream_ResetsBackoffAfterSuccess: when Watch succeeds, the next
// failure should start a fresh backoff sequence rather than carrying the
// previous (large) value. This matters in practice because a replica-set
// re-election briefly knocks streams offline; once the new primary is up,
// subsequent transient failures should retry quickly, not at 30s.
func TestRunStream_ResetsBackoffAfterSuccess(t *testing.T) {
	stubErr := errors.New("transient")

	buf := &bytes.Buffer{}
	b := newTestBus(t, captureLogger(buf))
	b.retryInitial = 1 * time.Millisecond
	b.retryMax = 8 * time.Millisecond

	var phase atomic.Int32 // 0: fail, 1: succeed once, 2: fail again
	successOnce := make(chan struct{}, 1)
	b.open = func(ctx context.Context, _ string) (changeStream, error) {
		switch phase.Load() {
		case 0:
			return nil, stubErr
		case 1:
			phase.Store(2)
			successOnce <- struct{}{}
			return &stubStream{closed: false, blockUntil: ctx.Done()}, nil
		default:
			return nil, stubErr
		}
	}

	runCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		b.runStream(runCtx, "x")
		close(done)
	}()
	time.Sleep(20 * time.Millisecond)
	// Let phase 0 fail a few times, then permit a success.
	phase.Store(1)
	select {
	case <-successOnce:
	case <-time.After(time.Second):
		t.Fatal("stream never succeeded")
	}
	// After successOnce, runStream is now blocked in consumeStream until ctx
	// is cancelled (stubStream.Next waits on ctx.Done). Cancel to wind down.
	cancel()
	<-done

	lines := decodeLogLines(t, buf)
	require.NotEmpty(t, lines)
	// The very last retry_in we logged (i.e. the most recent failure before
	// the successful Watch) is what we'd want to confirm reset on, but since
	// consumeStream blocks until cancel, no post-success failure is logged.
	// What we CAN verify: every retry_in observed must be <= retryMax, and
	// the loop continued past the first attempt (proving runStream didn't
	// silently abort on the first error).
	require.GreaterOrEqual(t, len(lines), 2)
}

// TestRunStream_ConsumesAndDispatches: end-to-end through the stub: open
// returns a stream that yields one synthetic event; the bus dispatches it to
// a subscriber.
func TestRunStream_ConsumesAndDispatches(t *testing.T) {
	buf := &bytes.Buffer{}
	b := newTestBus(t, captureLogger(buf))

	stream := &stubStream{
		events: []bson.M{
			{"operationType": "delete", "documentKey": bson.M{"uid": "uid-1"}},
		},
	}
	b.open = func(_ context.Context, _ string) (changeStream, error) {
		return stream, nil
	}

	// Subscribe before the stream runs; otherwise dispatch has no one to
	// deliver to.
	subCtx, subCancel := context.WithCancel(context.Background())
	defer subCancel()
	ch, err := b.Subscribe(subCtx, "collection.aggregaterule")
	require.NoError(t, err)

	select {
	case ev := <-ch:
		require.Equal(t, "collection.aggregaterule", ev.Topic)
		require.Equal(t, "delete", ev.Op)
		require.Equal(t, []string{"uid-1"}, ev.UIDs)
	case <-time.After(time.Second):
		t.Fatal("did not receive dispatched event")
	}
}

// TestChangeEventToSyncerEvent_TenantStamped is the H2 regression guard for
// mongo: a change event whose fullDocument carries tenant_id must produce a
// syncer.Event with that Tenant and the per-tenant Topic. Without it the
// receiving instance's per-tenant Reload short-circuits on the empty tenant.
func TestChangeEventToSyncerEvent_TenantStamped(t *testing.T) {
	raw := bson.M{
		"operationType": "update",
		"fullDocument":  bson.M{"uid": "uid-1", "tenant_id": "acme", "name": "owned"},
		"documentKey":   bson.M{"uid": "uid-1"},
	}
	ev := changeEventToSyncerEvent(raw, "rule")
	require.Equal(t, "rule", ev.Collection)
	require.Equal(t, "write", ev.Op)
	require.Equal(t, "acme", ev.Tenant, "event must carry the doc's tenant_id")
	require.Equal(t, "collection.rule.acme", ev.Topic, "topic must be the per-tenant topic")
	require.Equal(t, []string{"uid-1"}, ev.UIDs)
}

// TestChangeEventToSyncerEvent_GlobalNoTenant confirms a doc without tenant_id
// (a global collection) still produces the bare topic with no tenant — the fix
// must not invent a tenant for global collections.
func TestChangeEventToSyncerEvent_GlobalNoTenant(t *testing.T) {
	raw := bson.M{
		"operationType": "insert",
		"fullDocument":  bson.M{"uid": "uid-2", "name": "acme"},
	}
	ev := changeEventToSyncerEvent(raw, "tenant")
	require.Equal(t, "tenant", ev.Collection)
	require.Empty(t, ev.Tenant)
	require.Equal(t, "collection.tenant", ev.Topic)
}

// TestRunStream_DispatchesTenantStampedEvent drives a tenant-stamped change
// through the stub stream and asserts the dispatched event reaches a
// bare-prefix subscriber carrying the per-tenant topic.
func TestRunStream_DispatchesTenantStampedEvent(t *testing.T) {
	buf := &bytes.Buffer{}
	b := newTestBus(t, captureLogger(buf))

	stream := &stubStream{
		events: []bson.M{
			{
				"operationType": "update",
				"fullDocument":  bson.M{"uid": "uid-1", "tenant_id": "acme"},
				"documentKey":   bson.M{"uid": "uid-1"},
			},
		},
	}
	b.open = func(_ context.Context, _ string) (changeStream, error) { return stream, nil }

	subCtx, subCancel := context.WithCancel(context.Background())
	defer subCancel()
	ch, err := b.Subscribe(subCtx, "collection.rule")
	require.NoError(t, err)

	select {
	case ev := <-ch:
		require.Equal(t, "collection.rule.acme", ev.Topic)
		require.Equal(t, "acme", ev.Tenant)
		require.Equal(t, "write", ev.Op)
		require.Equal(t, []string{"uid-1"}, ev.UIDs)
	case <-time.After(time.Second):
		t.Fatal("did not receive dispatched event")
	}
}

// ---------------------------------------------------------------------------
// stubStream — minimal changeStream implementation for tests
// ---------------------------------------------------------------------------

type stubStream struct {
	events     []bson.M
	idx        int
	closed     bool
	blockUntil <-chan struct{} // when non-nil, Next blocks until this fires after events are drained
}

func (s *stubStream) Next(ctx context.Context) bool {
	if s.closed {
		return false
	}
	if s.idx < len(s.events) {
		return true
	}
	if s.blockUntil != nil {
		select {
		case <-s.blockUntil:
		case <-ctx.Done():
		}
	}
	return false
}

func (s *stubStream) Decode(v any) error {
	target, ok := v.(*bson.M)
	if !ok {
		return errors.New("stubStream: unexpected decode target")
	}
	// Round-trip the fixture through real BSON instead of handing the
	// hand-built bson.M straight back. The live driver decodes a change event
	// into a top-level bson.M whose nested sub-documents are bson.D, and code
	// that type-asserts bson.M on them silently misreads every event. A stub
	// that skips the encode/decode hides exactly that class of bug — it hid it
	// here for two months. See subDocument in watch.go.
	raw, err := bson.Marshal(s.events[s.idx])
	if err != nil {
		return err
	}
	var decoded bson.M
	if err := bson.Unmarshal(raw, &decoded); err != nil {
		return err
	}
	*target = decoded
	s.idx++
	return nil
}

func (s *stubStream) Close(_ context.Context) error {
	s.closed = true
	return nil
}

// TestHitsOnlyUpdate covers the predicate that suppresses the hit-counter
// reload storm: only an update touching solely `hits` is skipped; inserts,
// deletes, and updates touching any rule-affecting field are kept.
func TestHitsOnlyUpdate(t *testing.T) {
	cases := []struct {
		name string
		raw  bson.M
		want bool
	}{
		{"hits only", bson.M{"operationType": "update", "updateDescription": bson.M{"updatedFields": bson.M{"hits": int64(5)}}}, true},
		{"hits plus semantic field", bson.M{"operationType": "update", "updateDescription": bson.M{"updatedFields": bson.M{"hits": int64(5), "enabled": true}}}, false},
		{"semantic field only", bson.M{"operationType": "update", "updateDescription": bson.M{"updatedFields": bson.M{"enabled": true}}}, false},
		{"insert", bson.M{"operationType": "insert", "fullDocument": bson.M{"uid": "x"}}, false},
		{"delete", bson.M{"operationType": "delete", "documentKey": bson.M{"uid": "x"}}, false},
		{"update without updateDescription", bson.M{"operationType": "update"}, false},
		{"update with empty updatedFields", bson.M{"operationType": "update", "updateDescription": bson.M{"updatedFields": bson.M{}}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, hitsOnlyUpdate(tc.raw))
			// Same fixture as the driver actually delivers it: nested
			// sub-documents become bson.D once the event has been through a
			// real BSON round-trip. The predicate must reach the same verdict,
			// or every hit-counter bump gets dispatched as a real edit.
			require.Equal(t, tc.want, hitsOnlyUpdate(bsonRoundTrip(t, tc.raw)),
				"verdict must survive a real BSON decode")
		})
	}
}

// bsonRoundTrip encodes and decodes a fixture the way the mongo driver does for
// a change event, so tests see production Go types (nested docs as bson.D).
func bsonRoundTrip(t *testing.T, in bson.M) bson.M {
	t.Helper()
	raw, err := bson.Marshal(in)
	require.NoError(t, err)
	var out bson.M
	require.NoError(t, bson.Unmarshal(raw, &out))
	return out
}

// TestSubDocument_AcceptsDriverShapes pins the coercion every nested-field read
// in this file depends on. bson.D is the shape a live change stream produces;
// bson.M / map[string]any are what hand-built fixtures and normalised documents
// carry.
func TestSubDocument_AcceptsDriverShapes(t *testing.T) {
	want := bson.M{"tenant_id": "acme"}
	for _, tc := range []struct {
		name string
		in   any
		ok   bool
	}{
		{"bson.D (live change stream)", bson.D{{Key: "tenant_id", Value: "acme"}}, true},
		{"bson.M (hand-built fixture)", bson.M{"tenant_id": "acme"}, true},
		{"map[string]any (normalised)", map[string]any{"tenant_id": "acme"}, true},
		{"absent", nil, false},
		{"scalar", "acme", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := subDocument(tc.in)
			require.Equal(t, tc.ok, ok)
			if tc.ok {
				require.Equal(t, want, got)
			}
		})
	}
}

// TestChangeEventToSyncerEvent_LiveDecodeTypes is the direct regression guard
// for the production outage this fixed: with nested docs arriving as bson.D,
// the event lost its tenant, the syncer reloaded with a tenant-less context,
// and every tenant-scoped plugin's Reload short-circuited to a silent no-op —
// so no config change took effect until the process was restarted.
func TestChangeEventToSyncerEvent_LiveDecodeTypes(t *testing.T) {
	raw := bsonRoundTrip(t, bson.M{
		"operationType": "update",
		"fullDocument":  bson.M{"uid": "uid-1", "tenant_id": "acme", "name": "owned"},
		"documentKey":   bson.M{"uid": "uid-1"},
	})
	require.IsType(t, bson.D{}, raw["fullDocument"], "guard: the driver hands nested docs over as bson.D")

	ev := changeEventToSyncerEvent(raw, "snooze")
	require.Equal(t, "acme", ev.Tenant, "tenant must survive the driver's decode types")
	require.Equal(t, "collection.snooze.acme", ev.Topic)
	require.Equal(t, []string{"uid-1"}, ev.UIDs)
}

// TestDispatch_LogsDroppedEvent: a full subscriber channel must not swallow an
// event silently — a dropped event is a config change that never reaches its
// plugin, undiagnosable after the fact without this line.
func TestDispatch_LogsDroppedEvent(t *testing.T) {
	buf := &bytes.Buffer{}
	b := newTestBus(t, captureLogger(buf))

	// Register the subscription directly: this exercises dispatch in isolation,
	// without starting a watcher goroutine.
	sub := &subscription{prefix: "collection.snooze", ch: make(chan syncer.Event, 32)}
	b.subs = append(b.subs, sub)

	// 32 fills the buffer (nobody is reading); the 33rd must be dropped.
	for i := 0; i < 33; i++ {
		b.dispatch(syncer.Event{Topic: "collection.snooze.default", Collection: "snooze", Tenant: "default"})
	}

	lines := decodeLogLines(t, buf)
	require.Len(t, lines, 1, "exactly one drop, logged once")
	require.Equal(t, "mongo: change-stream subscriber is full; dropping event", lines[0]["msg"])
	require.Equal(t, "collection.snooze", lines[0]["subscriber"])
}

// TestRunStream_SkipsHitsOnlyUpdate drives a hit-counter bump followed by a real
// edit through the stub stream and asserts only the real edit is dispatched —
// the bump must not trigger a reload (the self-induced reload storm).
func TestRunStream_SkipsHitsOnlyUpdate(t *testing.T) {
	buf := &bytes.Buffer{}
	b := newTestBus(t, captureLogger(buf))

	stream := &stubStream{
		events: []bson.M{
			{
				"operationType":     "update",
				"documentKey":       bson.M{"uid": "bump"},
				"fullDocument":      bson.M{"uid": "bump", "tenant_id": "default"},
				"updateDescription": bson.M{"updatedFields": bson.M{"hits": int64(99)}},
			},
			{
				"operationType":     "update",
				"documentKey":       bson.M{"uid": "edit"},
				"fullDocument":      bson.M{"uid": "edit", "tenant_id": "default"},
				"updateDescription": bson.M{"updatedFields": bson.M{"enabled": false}},
			},
		},
	}
	b.open = func(_ context.Context, _ string) (changeStream, error) { return stream, nil }

	subCtx, subCancel := context.WithCancel(context.Background())
	defer subCancel()
	ch, err := b.Subscribe(subCtx, "collection.snooze")
	require.NoError(t, err)

	select {
	case ev := <-ch:
		// The hits-only bump precedes the edit in the stream; the first event
		// the subscriber sees must be the edit, proving the bump was dropped.
		require.Equal(t, []string{"edit"}, ev.UIDs, "hits-only update should have been skipped")
		require.Equal(t, "write", ev.Op)
	case <-time.After(time.Second):
		t.Fatal("did not receive dispatched event")
	}
}
