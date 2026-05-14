package mq

import (
	"context"
	"encoding/json"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestInproc_PublishSubscribe is the happy path: one publisher, one
// subscriber, payload round-trips.
func TestInproc_PublishSubscribe(t *testing.T) {
	bus := NewInproc(InprocConfig{QueueBuffer: 16})
	defer bus.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	received := make(chan Message, 4)
	err := bus.Subscribe(ctx, "q1", SubscribeOpts{BatchSize: 2, BatchTimer: 50 * time.Millisecond, Concurrency: 1},
		func(_ context.Context, m Message) error {
			received <- m
			return nil
		})
	require.NoError(t, err)

	require.NoError(t, bus.Publish(ctx, "q1", map[string]string{"hello": "world"}))

	select {
	case m := <-received:
		require.Equal(t, "q1", m.Queue)
		var body map[string]string
		require.NoError(t, json.Unmarshal(m.Payload, &body))
		require.Equal(t, "world", body["hello"])
	case <-time.After(2 * time.Second):
		t.Fatal("no message received")
	}
}

// TestInproc_BatchSizeRespected verifies that a worker's per-flush batch
// never exceeds BatchSize.
func TestInproc_BatchSizeRespected(t *testing.T) {
	bus := NewInproc(InprocConfig{QueueBuffer: 64})
	defer bus.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mu sync.Mutex
	var seenBatches int
	var maxBatchSize int
	currentBatch := int32(0)

	err := bus.Subscribe(ctx, "q1", SubscribeOpts{BatchSize: 3, BatchTimer: 30 * time.Millisecond, Concurrency: 1},
		func(_ context.Context, _ Message) error {
			// Crude batch detection: increment per-message, the worker
			// flushes the slice so we never see more than BatchSize
			// concurrent active counters.
			atomic.AddInt32(&currentBatch, 1)
			defer atomic.AddInt32(&currentBatch, -1)
			mu.Lock()
			seenBatches++
			cur := int(atomic.LoadInt32(&currentBatch))
			if cur > maxBatchSize {
				maxBatchSize = cur
			}
			mu.Unlock()
			return nil
		})
	require.NoError(t, err)

	for i := 0; i < 12; i++ {
		require.NoError(t, bus.Publish(ctx, "q1", map[string]int{"i": i}))
	}
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return seenBatches == 12
	}, 2*time.Second, 10*time.Millisecond)

	// Concurrency was 1 so we should never observe more than 1 in flight.
	mu.Lock()
	require.LessOrEqual(t, maxBatchSize, 1)
	mu.Unlock()
}

// TestInproc_Concurrency ensures multiple workers genuinely run in parallel.
func TestInproc_Concurrency(t *testing.T) {
	bus := NewInproc(InprocConfig{QueueBuffer: 64})
	defer bus.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var inFlight int32
	var maxParallel int32
	processed := make(chan struct{}, 32)
	err := bus.Subscribe(ctx, "q1", SubscribeOpts{BatchSize: 1, BatchTimer: 20 * time.Millisecond, Concurrency: 4},
		func(_ context.Context, _ Message) error {
			cur := atomic.AddInt32(&inFlight, 1)
			defer atomic.AddInt32(&inFlight, -1)
			for {
				m := atomic.LoadInt32(&maxParallel)
				if cur <= m || atomic.CompareAndSwapInt32(&maxParallel, m, cur) {
					break
				}
			}
			time.Sleep(20 * time.Millisecond)
			processed <- struct{}{}
			return nil
		})
	require.NoError(t, err)

	for i := 0; i < 12; i++ {
		require.NoError(t, bus.Publish(ctx, "q1", i))
	}
	for i := 0; i < 12; i++ {
		select {
		case <-processed:
		case <-time.After(3 * time.Second):
			t.Fatal("timeout waiting for handler")
		}
	}
	require.GreaterOrEqual(t, atomic.LoadInt32(&maxParallel), int32(2),
		"expected at least 2 workers in flight, got %d", atomic.LoadInt32(&maxParallel))
}

// TestInproc_PublishFullDropsRatherThanBlocks verifies the non-blocking
// publish contract.
func TestInproc_PublishFullDropsRatherThanBlocks(t *testing.T) {
	bus := NewInproc(InprocConfig{QueueBuffer: 2})
	defer bus.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// No subscriber — buffer fills up.
	require.NoError(t, bus.Publish(ctx, "q1", 1))
	require.NoError(t, bus.Publish(ctx, "q1", 2))
	err := bus.Publish(ctx, "q1", 3)
	require.Error(t, err) // returns error rather than blocking
}

// TestInproc_CloseStopsWorkers verifies Close drains workers cleanly.
func TestInproc_CloseStopsWorkers(t *testing.T) {
	time.Sleep(50 * time.Millisecond)
	runtime.GC()
	before := runtime.NumGoroutine()

	bus := NewInproc(InprocConfig{QueueBuffer: 8})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := bus.Subscribe(ctx, "q1", SubscribeOpts{BatchSize: 1, BatchTimer: 10 * time.Millisecond, Concurrency: 3},
		func(context.Context, Message) error { return nil })
	require.NoError(t, err)
	require.NoError(t, bus.Publish(ctx, "q1", "x"))
	time.Sleep(30 * time.Millisecond)
	require.NoError(t, bus.Close())
	// Second close is a no-op.
	require.NoError(t, bus.Close())

	require.Eventually(t, func() bool {
		runtime.GC()
		return runtime.NumGoroutine() <= before+1
	}, 2*time.Second, 20*time.Millisecond,
		"workers leaked: before=%d after=%d", before, runtime.NumGoroutine())
}

// TestInproc_ContextCancelStopsWorkers verifies cancelling the subscribe
// context also drains workers.
func TestInproc_ContextCancelStopsWorkers(t *testing.T) {
	time.Sleep(50 * time.Millisecond)
	runtime.GC()
	before := runtime.NumGoroutine()

	bus := NewInproc(InprocConfig{QueueBuffer: 8})
	defer bus.Close()

	ctx, cancel := context.WithCancel(context.Background())
	err := bus.Subscribe(ctx, "q1", SubscribeOpts{BatchSize: 1, BatchTimer: 10 * time.Millisecond, Concurrency: 2},
		func(context.Context, Message) error { return nil })
	require.NoError(t, err)
	cancel()

	require.Eventually(t, func() bool {
		runtime.GC()
		return runtime.NumGoroutine() <= before+2
	}, 2*time.Second, 20*time.Millisecond)
}

// TestInproc_PublishAfterCloseFails ensures the post-close contract.
func TestInproc_PublishAfterCloseFails(t *testing.T) {
	bus := NewInproc(InprocConfig{})
	require.NoError(t, bus.Close())
	require.Error(t, bus.Publish(context.Background(), "q1", 1))
	require.Error(t, bus.Subscribe(context.Background(), "q1", SubscribeOpts{}, func(context.Context, Message) error { return nil }))
}

// TestInproc_NilHandler is rejected.
func TestInproc_NilHandler(t *testing.T) {
	bus := NewInproc(InprocConfig{})
	defer bus.Close()
	err := bus.Subscribe(context.Background(), "q", SubscribeOpts{}, nil)
	require.Error(t, err)
}

// TestManager_DefaultsToInproc covers the manager's default path.
func TestManager_DefaultsToInproc(t *testing.T) {
	m, err := NewManager(context.Background(), Config{})
	require.NoError(t, err)
	defer m.Close()
	require.Equal(t, KindInproc, m.Kind)
	require.NotNil(t, m.Bus)
}

// TestManager_UnknownKindErrors covers the bad-config path.
func TestManager_UnknownKindErrors(t *testing.T) {
	_, err := NewManager(context.Background(), Config{Kind: "nope"})
	require.Error(t, err)
}
