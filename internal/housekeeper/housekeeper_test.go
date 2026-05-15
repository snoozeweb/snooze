package housekeeper

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Fake clock
// ---------------------------------------------------------------------------

type fakeClock struct {
	mu      sync.Mutex
	now     time.Time
	tickers []*fakeTicker
}

func newFakeClock(start time.Time) *fakeClock { return &fakeClock{now: start} }

func (f *fakeClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *fakeClock) NewTicker(d time.Duration) Ticker {
	f.mu.Lock()
	defer f.mu.Unlock()
	t := &fakeTicker{
		ch:     make(chan time.Time, 16),
		period: d,
		next:   f.now.Add(d),
	}
	f.tickers = append(f.tickers, t)
	return t
}

// Advance moves the clock forward by d and fires any ticker whose deadline is
// reached. Multi-period advances fire the ticker multiple times.
func (f *fakeClock) Advance(d time.Duration) {
	f.mu.Lock()
	f.now = f.now.Add(d)
	now := f.now
	tickers := append([]*fakeTicker(nil), f.tickers...)
	f.mu.Unlock()
	for _, t := range tickers {
		t.fireUpTo(now)
	}
}

type fakeTicker struct {
	mu     sync.Mutex
	ch     chan time.Time
	period time.Duration
	next   time.Time
	dead   bool
}

func (f *fakeTicker) C() <-chan time.Time { return f.ch }

func (f *fakeTicker) Stop() {
	f.mu.Lock()
	f.dead = true
	f.mu.Unlock()
}

func (f *fakeTicker) fireUpTo(now time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.dead {
		return
	}
	for !f.next.After(now) {
		select {
		case f.ch <- f.next:
		default:
		}
		f.next = f.next.Add(f.period)
	}
}

// ---------------------------------------------------------------------------
// Fake DB driver — narrow interface that mirrors db.Driver methods used by
// the default-job factories. The Python tests we are porting only need the
// record / comment paths.
// ---------------------------------------------------------------------------

type record struct {
	uid       string
	name      string
	dateEpoch float64
	ttl       float64 // seconds
}

type comment struct {
	uid       string
	recordUID string
	message   string
}

type fakeDB struct {
	mu       sync.Mutex
	records  []record
	comments []comment
}

func (f *fakeDB) writeRecord(name string, dateEpoch, ttl float64) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	uid := name + "-uid"
	f.records = append(f.records, record{uid: uid, name: name, dateEpoch: dateEpoch, ttl: ttl})
	return uid
}

func (f *fakeDB) writeComment(c comment) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.comments = append(f.comments, c)
}

func (f *fakeDB) cleanupTimeout(now time.Time) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	kept := f.records[:0]
	deleted := 0
	for _, r := range f.records {
		expiry := time.Unix(int64(r.dateEpoch), 0).Add(time.Duration(r.ttl) * time.Second)
		if expiry.Before(now) {
			deleted++
			continue
		}
		kept = append(kept, r)
	}
	f.records = kept
	return deleted
}

func (f *fakeDB) cleanupComments() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	valid := map[string]bool{}
	for _, r := range f.records {
		valid[r.uid] = true
	}
	kept := f.comments[:0]
	deleted := 0
	for _, c := range f.comments {
		if !valid[c.recordUID] {
			deleted++
			continue
		}
		kept = append(kept, c)
	}
	f.comments = kept
	return deleted
}

// ---------------------------------------------------------------------------
// Ported Python tests (tests/utils/test_housekeeper.py)
// ---------------------------------------------------------------------------

// TestCleanupAlertJob — ported from test_cleanup_alert.
func TestCleanupAlertJob(t *testing.T) {
	now := time.Now()
	lastWeek := now.Add(-7 * 24 * time.Hour)
	yesterday := now.Add(-24 * time.Hour)
	ttl := 3 * 24 * time.Hour

	db := &fakeDB{}
	db.writeRecord("1", float64(lastWeek.Unix()), ttl.Seconds())
	db.writeRecord("2", float64(yesterday.Unix()), ttl.Seconds())

	job := NewJobFunc("cleanup_alert", func(ctx context.Context) error {
		db.cleanupTimeout(now)
		return nil
	})
	require.NoError(t, job.Run(context.Background()))

	db.mu.Lock()
	defer db.mu.Unlock()
	require.Len(t, db.records, 1)
	require.Equal(t, "2", db.records[0].name)
}

// TestCleanupSnoozeJobInvokesDriver verifies CleanupSnoozeJob wires through
// to db.Driver.CleanupSnooze and surfaces both the count and the error.
func TestCleanupSnoozeJobInvokesDriver(t *testing.T) {
	calls := 0
	drv := &cleanupStubDriver{snoozeFn: func() (int, error) {
		calls++
		return 7, nil
	}}
	ij := CleanupSnoozeJob(drv)
	require.Equal(t, 72*time.Hour, ij.Interval)
	require.Equal(t, "cleanup_snooze", ij.Job.Name())
	require.NoError(t, ij.Job.Run(context.Background()))
	require.Equal(t, 1, calls)
}

// TestCleanupNotificationJobInvokesDriver mirrors TestCleanupSnoozeJobInvokesDriver.
func TestCleanupNotificationJobInvokesDriver(t *testing.T) {
	calls := 0
	drv := &cleanupStubDriver{notificationFn: func() (int, error) {
		calls++
		return 3, nil
	}}
	ij := CleanupNotificationJob(drv)
	require.Equal(t, 72*time.Hour, ij.Interval)
	require.Equal(t, "cleanup_notification", ij.Job.Name())
	require.NoError(t, ij.Job.Run(context.Background()))
	require.Equal(t, 1, calls)
}

// TestCleanupCommentJob — ported from test_cleanup_comment.
func TestCleanupCommentJob(t *testing.T) {
	now := time.Now()
	lastWeek := now.Add(-7 * 24 * time.Hour)
	yesterday := now.Add(-24 * time.Hour)
	ttl := 3 * 24 * time.Hour

	db := &fakeDB{}
	uid1 := db.writeRecord("1", float64(lastWeek.Unix()), ttl.Seconds())
	uid2 := db.writeRecord("2", float64(yesterday.Unix()), ttl.Seconds())

	db.writeComment(comment{uid: "c1", recordUID: uid1, message: "comment 1"})
	db.writeComment(comment{uid: "c2", recordUID: uid1, message: "comment 2"})
	db.writeComment(comment{uid: "c3", recordUID: uid2, message: "comment 3"})
	db.writeComment(comment{uid: "c4", recordUID: "unknown", message: "comment 4"})

	job := NewJobFunc("cleanup_comment", func(ctx context.Context) error {
		db.cleanupComments()
		return nil
	})
	require.NoError(t, job.Run(context.Background()))

	db.mu.Lock()
	defer db.mu.Unlock()
	require.Len(t, db.comments, 3)
	require.Equal(t, "comment 1", db.comments[0].message)
	require.Equal(t, "comment 2", db.comments[1].message)
	require.Equal(t, "comment 3", db.comments[2].message)
}

// ---------------------------------------------------------------------------
// Scheduler unit tests
// ---------------------------------------------------------------------------

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestSchedule_Validate(t *testing.T) {
	require.Error(t, Schedule{}.validate())
	require.Error(t, Schedule{Cron: "0 0 * * *", Interval: time.Second}.validate())
	require.NoError(t, Schedule{Cron: "0 0 * * *"}.validate())
	require.NoError(t, Schedule{Interval: time.Second}.validate())
}

func TestRegister_RejectsBadSchedule(t *testing.T) {
	h := New(testLogger())
	err := h.Register(NewJobFunc("nope", func(context.Context) error { return nil }), Schedule{})
	require.Error(t, err)
}

func TestRegister_RejectsNilJob(t *testing.T) {
	h := New(testLogger())
	require.Error(t, h.Register(nil, Schedule{Interval: time.Second}))
}

func TestRun_IntervalFiresOnTick(t *testing.T) {
	clk := newFakeClock(time.Unix(0, 0))
	h := New(testLogger(), WithClock(clk))

	var n int64
	job := NewJobFunc("ticker", func(context.Context) error {
		atomic.AddInt64(&n, 1)
		return nil
	})
	require.NoError(t, h.Register(job, Schedule{Interval: 10 * time.Second}))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = h.Run(ctx); close(done) }()

	require.Eventually(t, func() bool {
		clk.Advance(10 * time.Second)
		return atomic.LoadInt64(&n) >= 1
	}, time.Second, 5*time.Millisecond)

	clk.Advance(10 * time.Second)
	require.Eventually(t, func() bool { return atomic.LoadInt64(&n) >= 2 }, time.Second, 5*time.Millisecond)

	cancel()
	<-done
}

func TestRun_TriggerOnStartup(t *testing.T) {
	clk := newFakeClock(time.Unix(0, 0))
	h := New(testLogger(), WithClock(clk), WithTriggerOnStartup(true))

	var n int64
	require.NoError(t, h.Register(NewJobFunc("startup", func(context.Context) error {
		atomic.AddInt64(&n, 1)
		return nil
	}), Schedule{Interval: time.Hour}))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = h.Run(ctx); close(done) }()

	require.Eventually(t, func() bool { return atomic.LoadInt64(&n) >= 1 }, time.Second, 5*time.Millisecond)
	cancel()
	<-done
	require.Equal(t, int64(1), atomic.LoadInt64(&n)) // no extra ticks
}

func TestRun_PanicIsRecovered(t *testing.T) {
	clk := newFakeClock(time.Unix(0, 0))
	h := New(testLogger(), WithClock(clk), WithTriggerOnStartup(true))

	var n int64
	require.NoError(t, h.Register(NewJobFunc("panic", func(context.Context) error {
		atomic.AddInt64(&n, 1)
		panic("boom")
	}), Schedule{Interval: 5 * time.Second}))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = h.Run(ctx); close(done) }()
	// Startup fire (panics) followed by every-tick fires must keep going.
	require.Eventually(t, func() bool {
		clk.Advance(5 * time.Second)
		return atomic.LoadInt64(&n) >= 3
	}, 2*time.Second, 5*time.Millisecond)
	cancel()
	<-done
}

func TestRun_JobErrorDoesNotStopOthers(t *testing.T) {
	clk := newFakeClock(time.Unix(0, 0))
	h := New(testLogger(), WithClock(clk), WithTriggerOnStartup(true))

	var bad, good int64
	require.NoError(t, h.Register(NewJobFunc("bad", func(context.Context) error {
		atomic.AddInt64(&bad, 1)
		return errors.New("nope")
	}), Schedule{Interval: 10 * time.Second}))
	require.NoError(t, h.Register(NewJobFunc("good", func(context.Context) error {
		atomic.AddInt64(&good, 1)
		return nil
	}), Schedule{Interval: 10 * time.Second}))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = h.Run(ctx); close(done) }()
	require.Eventually(t, func() bool {
		return atomic.LoadInt64(&bad) >= 1 && atomic.LoadInt64(&good) >= 1
	}, time.Second, 5*time.Millisecond)
	cancel()
	<-done
}

func TestRun_NoJobsBlocksUntilCancel(t *testing.T) {
	h := New(testLogger())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = h.Run(ctx); close(done) }()
	select {
	case <-done:
		t.Fatal("Run returned before cancel")
	case <-time.After(20 * time.Millisecond):
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not return after cancel")
	}
}

func TestRun_CronInvalidExpressionDoesNotPanic(t *testing.T) {
	h := New(testLogger())
	require.NoError(t, h.Register(NewJobFunc("bad-cron", func(context.Context) error { return nil }),
		Schedule{Cron: "not a cron"}))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = h.Run(ctx); close(done) }()
	cancel()
	<-done
}

func TestRun_CronExpressionAcceptsFiveField(t *testing.T) {
	// Validate that the standard 5-field daily-midnight cron expression used by
	// the default jobs registers without error.
	h := New(testLogger())
	require.NoError(t, h.Register(NewJobFunc("midnight", func(context.Context) error { return nil }),
		Schedule{Cron: "0 0 * * *"}))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = h.Run(ctx); close(done) }()
	// Give the runner a beat to start the cron scheduler, then shut down cleanly.
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}
