// Package housekeeper schedules periodic database-maintenance jobs.
//
// It replaces src/snooze/utils/housekeeper.py. Two scheduling shapes are
// supported per job:
//
//   - interval — runs every Schedule.Interval, starting Schedule.Interval after
//     Run begins (or immediately when TriggerOnStartup is set).
//   - cron — driven by github.com/robfig/cron/v3 with second-precision parsing.
//
// Jobs run sequentially on their own goroutine; a single slow job cannot delay
// another. Panics inside a job are recovered and logged.
package housekeeper

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

// Job is the contract every scheduled task implements.
//
// Name is used for logging and metrics. Run must respect ctx cancellation; the
// housekeeper waits for the running job to return before stopping.
type Job interface {
	Name() string
	Run(ctx context.Context) error
}

// Schedule selects between cron and fixed-interval cadence. Exactly one of
// Cron, Interval, or LiveInterval must be set.
//
// LiveInterval is a closure that returns the current desired cadence; it is
// consulted before every fire so the job adapts to runtime-edited settings
// without a restart. The poll cadence is fixed at LivePollInterval (or
// 30 seconds when zero); jobs whose live interval is shorter than the poll
// cadence will see at most one fire per poll.
type Schedule struct {
	Cron             string
	Interval         time.Duration
	LiveInterval     func(ctx context.Context) time.Duration
	LivePollInterval time.Duration
}

// validate returns nil when exactly one schedule shape is configured.
func (s Schedule) validate() error {
	hasCron := s.Cron != ""
	hasInterval := s.Interval > 0
	hasLive := s.LiveInterval != nil
	count := 0
	if hasCron {
		count++
	}
	if hasInterval {
		count++
	}
	if hasLive {
		count++
	}
	switch count {
	case 0:
		return errors.New("housekeeper: schedule has no Cron / Interval / LiveInterval set")
	case 1:
		return nil
	default:
		return errors.New("housekeeper: schedule has multiple cadence fields set")
	}
}

// Clock abstracts time for deterministic tests.
//
// Production code uses systemClock; tests construct a FakeClock and inject it
// via WithClock.
type Clock interface {
	Now() time.Time
	NewTicker(d time.Duration) Ticker
}

// Ticker is the small slice of time.Ticker the housekeeper depends on.
type Ticker interface {
	C() <-chan time.Time
	Stop()
}

type systemClock struct{}

// SystemClock returns the production wall-clock implementation of Clock. Jobs
// that take a Clock directly (e.g. EscalateTimeoutJob, whose deadline math must
// be fakeable in tests) get this at boot, while the Housekeeper itself defaults
// to the same clock internally.
func SystemClock() Clock { return systemClock{} }

func (systemClock) Now() time.Time { return time.Now() }
func (systemClock) NewTicker(d time.Duration) Ticker {
	t := time.NewTicker(d)
	return realTicker{t: t}
}

type realTicker struct{ t *time.Ticker }

func (r realTicker) C() <-chan time.Time { return r.t.C }
func (r realTicker) Stop()               { r.t.Stop() }

// Housekeeper holds the set of scheduled jobs and drives them.
type Housekeeper struct {
	log              *slog.Logger
	clock            Clock
	triggerOnStartup bool

	mu      sync.Mutex
	entries []entry
}

type entry struct {
	job      Job
	schedule Schedule
}

// Option configures Housekeeper construction.
type Option func(*Housekeeper)

// WithClock injects a custom clock (used by tests).
func WithClock(c Clock) Option { return func(h *Housekeeper) { h.clock = c } }

// WithTriggerOnStartup makes interval jobs fire once immediately when Run
// begins, before waiting for the first tick. Cron jobs ignore this flag.
func WithTriggerOnStartup(v bool) Option {
	return func(h *Housekeeper) { h.triggerOnStartup = v }
}

// New constructs a Housekeeper with the given logger.
func New(logger *slog.Logger, opts ...Option) *Housekeeper {
	if logger == nil {
		logger = slog.Default()
	}
	h := &Housekeeper{log: logger, clock: systemClock{}}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// Register adds a job to the schedule. Registration is only valid before Run
// is called; Register itself is safe to call concurrently.
func (h *Housekeeper) Register(j Job, s Schedule) error {
	if j == nil {
		return errors.New("housekeeper: nil job")
	}
	if err := s.validate(); err != nil {
		return fmt.Errorf("housekeeper: register %q: %w", j.Name(), err)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.entries = append(h.entries, entry{job: j, schedule: s})
	return nil
}

// Run drives every registered job until ctx is cancelled. It blocks for the
// caller's lifetime and returns nil when ctx is done. Errors from individual
// jobs are logged but never propagated — one bad job must not stop the rest.
func (h *Housekeeper) Run(ctx context.Context) error {
	h.mu.Lock()
	entries := append([]entry(nil), h.entries...)
	h.mu.Unlock()

	if len(entries) == 0 {
		<-ctx.Done()
		return nil
	}

	cronRunner := cron.New(cron.WithParser(cronParser()))
	cronAdded := false
	var wg sync.WaitGroup

	for _, e := range entries {
		switch {
		case e.schedule.LiveInterval != nil:
			wg.Add(1)
			go func() {
				defer wg.Done()
				h.runLive(ctx, e.job, e.schedule)
			}()
		case e.schedule.Interval > 0:
			wg.Add(1)
			go func() {
				defer wg.Done()
				h.runInterval(ctx, e.job, e.schedule.Interval)
			}()
		case e.schedule.Cron != "":
			if _, err := cronRunner.AddFunc(e.schedule.Cron, func() {
				h.invoke(ctx, e.job)
			}); err != nil {
				h.log.Error("housekeeper: invalid cron expression",
					"job", e.job.Name(), "expr", e.schedule.Cron, "err", err)
				continue
			}
			cronAdded = true
		}
	}

	if cronAdded {
		cronRunner.Start()
	}

	<-ctx.Done()

	if cronAdded {
		stopCtx := cronRunner.Stop()
		<-stopCtx.Done()
	}
	wg.Wait()
	return nil
}

func (h *Housekeeper) runInterval(ctx context.Context, j Job, d time.Duration) {
	if h.triggerOnStartup {
		h.invoke(ctx, j)
	}
	t := h.clock.NewTicker(d)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C():
			h.invoke(ctx, j)
		}
	}
}

// runLive drives a job whose desired cadence is supplied by a closure
// consulted at every poll. The ticker fires every poll interval (default
// 30s) and we run the job iff at least its currently-configured interval
// has elapsed since the last fire. Operators can therefore widen or
// narrow a cleanup cadence from the UI and the next firing reflects the
// new value within one poll period.
func (h *Housekeeper) runLive(ctx context.Context, j Job, s Schedule) {
	poll := s.LivePollInterval
	if poll <= 0 {
		poll = 30 * time.Second
	}
	var last time.Time
	if h.triggerOnStartup {
		h.invoke(ctx, j)
		last = h.clock.Now()
	}
	t := h.clock.NewTicker(poll)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C():
			interval := s.LiveInterval(ctx)
			if interval <= 0 {
				// Job is disabled at the moment; keep polling.
				continue
			}
			if last.IsZero() || now.Sub(last) >= interval {
				h.invoke(ctx, j)
				last = h.clock.Now()
			}
		}
	}
}

// JobResult is the per-job outcome of a RunAll cycle. Error is empty on
// success; DurationMs is the wall-clock time the job's Run took, in
// milliseconds (always >= 0). The struct is JSON-marshalled directly by the
// HTTP layer.
type JobResult struct {
	Name       string `json:"name"`
	Error      string `json:"error,omitempty"`
	DurationMs int64  `json:"duration_ms"`
}

// RegisteredJobs returns the number of jobs currently registered. A nil
// receiver reports 0. Safe to call concurrently.
func (h *Housekeeper) RegisteredJobs() int {
	if h == nil {
		return 0
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.entries)
}

// RunAll fires every registered job once, synchronously and sequentially, and
// returns one JobResult per job. It is the on-demand counterpart to Run: the
// caller blocks until every job has finished. Per-job errors (and recovered
// panics) are captured in JobResult.Error rather than propagated — RunAll never
// returns an error and never panics. A nil receiver or empty registry yields an
// empty slice.
//
// RunAll does not coordinate with the scheduler goroutine: a job may run here
// while Run is also firing it. Jobs are idempotent DB sweeps, so overlapping
// runs are benign (already-absent rows are a no-op on all backends).
func (h *Housekeeper) RunAll(ctx context.Context) []JobResult {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	entries := append([]entry(nil), h.entries...)
	h.mu.Unlock()

	results := make([]JobResult, 0, len(entries))
	for _, e := range entries {
		results = append(results, h.runOnce(ctx, e.job))
	}
	return results
}

// runOnce invokes a single job with the same panic-recover semantics as invoke,
// timing it via the package clock, and returns the captured outcome.
func (h *Housekeeper) runOnce(ctx context.Context, j Job) (res JobResult) {
	res.Name = j.Name()
	start := h.clock.Now()
	defer func() {
		res.DurationMs = h.clock.Now().Sub(start).Milliseconds()
		if r := recover(); r != nil {
			h.log.Error("housekeeper: job panicked", "job", j.Name(), "panic", r)
			res.Error = fmt.Sprintf("panic: %v", r)
		}
	}()
	if err := j.Run(ctx); err != nil {
		h.log.Error("housekeeper: job failed", "job", j.Name(), "err", err)
		res.Error = err.Error()
	}
	return res
}

func (h *Housekeeper) invoke(ctx context.Context, j Job) {
	defer func() {
		if r := recover(); r != nil {
			h.log.Error("housekeeper: job panicked", "job", j.Name(), "panic", r)
		}
	}()
	if ctx.Err() != nil {
		return
	}
	start := h.clock.Now()
	if err := j.Run(ctx); err != nil {
		h.log.Error("housekeeper: job failed", "job", j.Name(), "err", err)
		return
	}
	h.log.Debug("housekeeper: job done", "job", j.Name(), "duration", h.clock.Now().Sub(start))
}

// cronParser accepts second-precision specs (`* * * * * *`) and the classic
// five-field form (`* * * * *`, treated as second=0).
func cronParser() cron.ScheduleParser {
	return cron.NewParser(
		cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
	)
}
