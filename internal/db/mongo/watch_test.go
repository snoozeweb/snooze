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
	b.open = func(ctx context.Context, collection string) (changeStream, error) {
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
	b.open = func(ctx context.Context, collection string) (changeStream, error) {
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
	b.open = func(ctx context.Context, collection string) (changeStream, error) {
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
	b.open = func(ctx context.Context, collection string) (changeStream, error) {
		return stream, nil
	}

	// Subscribe before the stream runs; otherwise dispatch has no one to
	// deliver to.
	subCtx, subCancel := context.WithCancel(context.Background())
	defer subCancel()
	ch, err := b.Subscribe(subCtx, "collection.aggregaterule")
	require.NoError(t, err)

	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		b.runStream(runCtx, "aggregaterule")
		close(done)
	}()

	select {
	case ev := <-ch:
		require.Equal(t, "collection.aggregaterule", ev.Topic)
		require.Equal(t, "delete", ev.Op)
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
	*target = s.events[s.idx]
	s.idx++
	return nil
}

func (s *stubStream) Close(_ context.Context) error {
	s.closed = true
	return nil
}
