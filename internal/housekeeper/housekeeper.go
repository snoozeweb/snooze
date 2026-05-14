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
// Cron or Interval must be set.
type Schedule struct {
	Cron     string
	Interval time.Duration
}

// validate returns nil when exactly one shape is configured.
func (s Schedule) validate() error {
	hasCron := s.Cron != ""
	hasInterval := s.Interval > 0
	switch {
	case hasCron && hasInterval:
		return errors.New("housekeeper: schedule has both Cron and Interval set")
	case !hasCron && !hasInterval:
		return errors.New("housekeeper: schedule has neither Cron nor Interval set")
	default:
		return nil
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

func (systemClock) Now() time.Time { return time.Now() }
func (systemClock) NewTicker(d time.Duration) Ticker {
	t := time.NewTicker(d)
	return realTicker{t: t}
}

type realTicker struct{ t *time.Ticker }

func (r realTicker) C() <-chan time.Time { return r.t.C }
func (r realTicker) Stop()                { r.t.Stop() }

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
		e := e
		switch {
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
