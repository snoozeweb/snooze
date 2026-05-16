package core

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/snoozeweb/snooze/internal/telemetry"
)

// fastBackoff is a 1µs / 1µs / 1ms policy used to keep the supervisor tests
// snappy. Tries=3 mirrors the production default.
func fastBackoff() BackoffPolicy {
	return BackoffPolicy{
		Initial:    time.Microsecond,
		Max:        time.Millisecond,
		Multiplier: 2,
		Tries:      3,
		Window:     time.Second,
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestSupervisor_CleanExit(t *testing.T) {
	t.Parallel()
	sup := &Supervisor{Logger: discardLogger()}
	g, gctx := errgroup.WithContext(context.Background())
	called := atomic.Int32{}
	sup.Go(gctx, g, Job{
		Name: "clean",
		Fn: func(_ context.Context) error {
			called.Add(1)
			return nil
		},
	})
	require.NoError(t, g.Wait())
	require.EqualValues(t, 1, called.Load())
}

func TestSupervisor_ContextCancellation(t *testing.T) {
	t.Parallel()
	sup := &Supervisor{Logger: discardLogger()}
	ctx, cancel := context.WithCancel(context.Background())
	g, gctx := errgroup.WithContext(ctx)

	started := make(chan struct{})
	saw := make(chan struct{})
	sup.Go(gctx, g, Job{
		Name: "ctx-cancel",
		Fn: func(ctx context.Context) error {
			close(started)
			<-ctx.Done()
			close(saw)
			return ctx.Err()
		},
	})
	<-started
	cancel()
	// The supervisor swallows ctx-cancellation errors (errgroup itself drives
	// the cancellation semantics for siblings). Wait must return nil and the
	// goroutine must have observed ctx.Done().
	require.NoError(t, g.Wait())
	select {
	case <-saw:
	default:
		t.Fatal("job did not observe ctx cancellation")
	}
}

func TestSupervisor_PanicRecovered(t *testing.T) {
	t.Parallel()
	reg := telemetry.NewRegistry(nil)
	sup := &Supervisor{Logger: discardLogger(), Metrics: reg}

	calls := atomic.Int32{}
	g, gctx := errgroup.WithContext(context.Background())
	sup.Go(gctx, g, Job{
		Name:    "panicker",
		Backoff: fastBackoff(),
		Fn: func(_ context.Context) error {
			n := calls.Add(1)
			if n <= 2 {
				panic("kaboom")
			}
			return nil
		},
	})
	require.NoError(t, g.Wait())
	require.EqualValues(t, 3, calls.Load())

	statuses := sup.Status()
	require.Len(t, statuses, 1)
	require.Equal(t, "panicker", statuses[0].Name)
	require.GreaterOrEqual(t, statuses[0].Restarts, 2)
}

func TestSupervisor_BoundedRetry_NonCritical(t *testing.T) {
	t.Parallel()
	sup := &Supervisor{Logger: discardLogger()}

	calls := atomic.Int32{}
	g, gctx := errgroup.WithContext(context.Background())
	sup.Go(gctx, g, Job{
		Name:    "flaky",
		Backoff: fastBackoff(),
		Fn: func(_ context.Context) error {
			calls.Add(1)
			return errors.New("nope")
		},
	})
	// Non-critical: must NOT propagate; errgroup returns nil.
	require.NoError(t, g.Wait())
	// Tries=3 → fail #4 triggers give-up (failures>policy.Tries).
	require.EqualValues(t, 4, calls.Load())
}

func TestSupervisor_BoundedRetry_CriticalPropagates(t *testing.T) {
	t.Parallel()
	sup := &Supervisor{Logger: discardLogger()}

	wantErr := errors.New("boom")
	calls := atomic.Int32{}
	g, gctx := errgroup.WithContext(context.Background())
	sup.Go(gctx, g, Job{
		Name:     "critical-flaky",
		Critical: true,
		Backoff:  fastBackoff(),
		Fn: func(_ context.Context) error {
			calls.Add(1)
			return wantErr
		},
	})
	err := g.Wait()
	require.Error(t, err)
	require.ErrorIs(t, err, wantErr)
	require.GreaterOrEqual(t, calls.Load(), int32(4))
}

func TestSupervisor_NoFnIsSilent(t *testing.T) {
	t.Parallel()
	sup := &Supervisor{Logger: discardLogger()}
	g, gctx := errgroup.WithContext(context.Background())
	sup.Go(gctx, g, Job{Name: "no-fn"})
	require.NoError(t, g.Wait())
}
