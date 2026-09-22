package plugins

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The regression this file exists for: a waiter whose context expired used to
// stay parked inside sync.WaitGroup.Wait, so the next dispatch's Add raced it.
// Under -race that surfaced as an intermittent failure in whichever two
// TestBuild_* tests -shuffle happened to interleave.
func TestBackgroundTracker_AbandonedWaitDoesNotRaceNextAdd(t *testing.T) {
	t.Parallel()
	var tr backgroundTracker

	// One job in flight that we never let finish until the end.
	release := make(chan struct{})
	tr.add()
	go func() {
		<-release
		tr.done()
	}()

	// A waiter gives up before the job finishes.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, tr.wait(ctx), context.DeadlineExceeded)

	// A second dispatch starts while the first is still running and the
	// abandoned waiter is (previously) still parked.
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tr.add()
			tr.done()
		}()
	}
	wg.Wait()

	close(release)
	require.NoError(t, tr.wait(context.Background()))
}

func TestBackgroundTracker_WaitReturnsImmediatelyWhenIdle(t *testing.T) {
	t.Parallel()
	var tr backgroundTracker

	// Never used.
	require.NoError(t, tr.wait(context.Background()))

	// Drained.
	tr.add()
	tr.done()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // even an already-dead context must not turn an idle wait into an error
	require.NoError(t, tr.wait(ctx))
}

func TestBackgroundTracker_WaitBlocksUntilLastJobReturns(t *testing.T) {
	t.Parallel()
	var tr backgroundTracker

	tr.add()
	tr.add()
	finished := make(chan struct{})
	go func() {
		require.NoError(t, tr.wait(context.Background()))
		close(finished)
	}()

	tr.done()
	select {
	case <-finished:
		t.Fatal("wait returned while a job was still in flight")
	case <-time.After(20 * time.Millisecond):
	}

	tr.done()
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("wait did not return after the last job finished")
	}
}

// resetForTest rebuilds the registry around goroutines that may still be
// running, so a completion arriving after the reset must not panic or push the
// counter negative.
func TestBackgroundTracker_StrayDoneIsIgnored(t *testing.T) {
	t.Parallel()
	var tr backgroundTracker

	require.NotPanics(t, tr.done)
	tr.add()
	tr.done()
	require.NotPanics(t, tr.done)
	// Still usable afterwards: the counter did not go negative.
	tr.add()
	blocked := make(chan struct{})
	go func() {
		require.NoError(t, tr.wait(context.Background()))
		close(blocked)
	}()
	select {
	case <-blocked:
		t.Fatal("wait returned though a job is in flight — counter went negative")
	case <-time.After(20 * time.Millisecond):
	}
	tr.done()
	<-blocked
}
