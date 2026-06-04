package syncer

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/config/schema"
)

// recordingPersister captures Persist invocations for assertions.
type recordingPersister struct {
	mu     sync.Mutex
	calls  []persistCall
	err    error
	hits   int64
	notify chan struct{}
}

type persistCall struct {
	Collection string
	Doc        map[string]any
}

func newRecordingPersister() *recordingPersister {
	return &recordingPersister{notify: make(chan struct{}, 64)}
}

func (r *recordingPersister) Fn() HeartbeatPersister {
	return func(_ context.Context, collection string, doc map[string]any) error {
		r.mu.Lock()
		// Copy doc to dodge later mutation by the caller.
		cp := make(map[string]any, len(doc))
		for k, v := range doc {
			cp[k] = v
		}
		r.calls = append(r.calls, persistCall{Collection: collection, Doc: cp})
		err := r.err
		r.mu.Unlock()
		atomic.AddInt64(&r.hits, 1)
		select {
		case r.notify <- struct{}{}:
		default:
		}
		return err
	}
}

func (r *recordingPersister) Count() int64 { return atomic.LoadInt64(&r.hits) }

func (r *recordingPersister) Last() (persistCall, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.calls) == 0 {
		return persistCall{}, false
	}
	return r.calls[len(r.calls)-1], true
}

// TestNodeHeartbeat_FirstWriteThenTicks verifies that an initial write fires
// synchronously and subsequent ticks keep going until ctx is cancelled.
func TestNodeHeartbeat_FirstWriteThenTicks(t *testing.T) {
	rp := newRecordingPersister()
	hb := &NodeHeartbeat{
		Persist:  rp.Fn(),
		Node:     "node-A",
		Version:  "1.2.3",
		Interval: 25 * time.Millisecond,
		Logger:   quietLogger(),
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- hb.Run(ctx) }()

	// Initial write fires before the first tick.
	require.Eventually(t, func() bool { return rp.Count() >= 1 },
		time.Second, 5*time.Millisecond, "no initial heartbeat")
	last, _ := rp.Last()
	require.Equal(t, "nodes", last.Collection)
	require.Equal(t, "node-A", last.Doc["node"])
	require.Equal(t, "1.2.3", last.Doc["version"])
	require.NotEmpty(t, last.Doc["started_at"])
	require.NotEmpty(t, last.Doc["last_seen"])

	// At least one additional tick.
	require.Eventually(t, func() bool { return rp.Count() >= 3 },
		2*time.Second, 10*time.Millisecond)

	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("Run did not exit after cancel")
	}
}

// TestNodeHeartbeat_DefaultsHostname verifies that an empty Node defaults to
// schema.DefaultHostname() — the single source of truth shared with the config
// layer (OS hostname, falling back to "snooze"). This guards against the prior
// mismatch where the heartbeat fell back to "unknown".
func TestNodeHeartbeat_DefaultsHostname(t *testing.T) {
	rp := newRecordingPersister()
	hb := &NodeHeartbeat{
		Persist:  rp.Fn(),
		Interval: 50 * time.Millisecond,
		Logger:   quietLogger(),
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- hb.Run(ctx) }()

	require.Eventually(t, func() bool { return rp.Count() >= 1 },
		time.Second, 5*time.Millisecond)
	last, _ := rp.Last()
	require.NotEmpty(t, last.Doc["node"])
	require.Equal(t, schema.DefaultHostname(), last.Doc["node"],
		"empty Node must fall back to the shared schema.DefaultHostname")
	require.NotEmpty(t, last.Doc["version"]) // defaults to version.String()

	cancel()
	<-done
}

// TestNodeHeartbeat_NilPersist returns an error.
func TestNodeHeartbeat_NilPersist(t *testing.T) {
	hb := &NodeHeartbeat{}
	require.Error(t, hb.Run(context.Background()))
}
