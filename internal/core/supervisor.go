package core

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/snoozeweb/snooze/internal/telemetry"
)

// Job describes a single supervised goroutine. Fn must respect ctx
// cancellation. On panic the supervisor logs the stack and bumps the
// SupervisorPanic counter; on non-nil error it consults Backoff before
// either retrying, propagating (Critical), or giving up.
type Job struct {
	Name     string
	Critical bool
	Fn       func(context.Context) error
	Backoff  BackoffPolicy
}

// BackoffPolicy controls retry timing. The defaults mirror the Python
// `RateLimit(3 fails / 1min)` strategy: at most 3 retries within a 60s
// window, starting from a 1s sleep, doubling up to 30s.
type BackoffPolicy struct {
	Initial    time.Duration
	Max        time.Duration
	Multiplier float64
	Tries      int
	Window     time.Duration
}

// defaultBackoff returns the canonical policy applied to Jobs that leave
// BackoffPolicy zero-valued.
func defaultBackoff() BackoffPolicy {
	return BackoffPolicy{
		Initial:    time.Second,
		Max:        30 * time.Second,
		Multiplier: 2,
		Tries:      3,
		Window:     time.Minute,
	}
}

// withDefaults fills in zero fields from defaultBackoff.
func (b BackoffPolicy) withDefaults() BackoffPolicy {
	d := defaultBackoff()
	if b.Initial <= 0 {
		b.Initial = d.Initial
	}
	if b.Max <= 0 {
		b.Max = d.Max
	}
	if b.Multiplier <= 1 {
		b.Multiplier = d.Multiplier
	}
	if b.Tries <= 0 {
		b.Tries = d.Tries
	}
	if b.Window <= 0 {
		b.Window = d.Window
	}
	return b
}

// JobStatus is the public snapshot of one supervised job.
type JobStatus struct {
	Name        string
	Running     bool
	Restarts    int
	LastError   error
	LastErrorAt time.Time
}

// Supervisor wraps a set of Jobs running on an errgroup.Group. It is safe for
// concurrent use after construction.
type Supervisor struct {
	Logger  *slog.Logger
	Metrics *telemetry.Registry

	mu       sync.Mutex
	statuses map[string]*JobStatus
}

// Status returns a stable snapshot of every job's current state.
func (s *Supervisor) Status() []JobStatus {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]JobStatus, 0, len(s.statuses))
	for _, st := range s.statuses {
		out = append(out, *st)
	}
	return out
}

// Go enrolls j on g with panic recovery + bounded retry. The returned
// goroutine respects ctx cancellation in two ways: (a) Fn must do so, and
// (b) on Fn returning an error the supervisor checks ctx before retrying.
func (s *Supervisor) Go(ctx context.Context, g *errgroup.Group, j Job) {
	if g == nil {
		return
	}
	if j.Fn == nil {
		return
	}
	if s == nil {
		s = &Supervisor{}
	}
	if s.Logger == nil {
		s.Logger = slog.Default()
	}
	policy := j.Backoff.withDefaults()
	s.touch(j.Name, func(st *JobStatus) { st.Running = false })

	g.Go(func() error {
		return s.run(ctx, j, policy)
	})
}

// run is the per-job retry loop. Returns the (possibly nil) error that should
// be propagated to the errgroup.
func (s *Supervisor) run(ctx context.Context, j Job, policy BackoffPolicy) error {
	sleep := policy.Initial
	var failures []time.Time

	for {
		if ctx.Err() != nil {
			s.touch(j.Name, func(st *JobStatus) { st.Running = false })
			return nil
		}

		s.touch(j.Name, func(st *JobStatus) { st.Running = true })
		err := s.invoke(ctx, j)
		s.touch(j.Name, func(st *JobStatus) { st.Running = false })

		if err == nil {
			// Clean exit. Errgroup waits for the rest.
			return nil
		}

		if ctx.Err() != nil {
			// The error is almost certainly ctx.Err(). Swallow it; the
			// errgroup will return ctx.Err itself.
			return nil
		}

		now := time.Now()
		failures = append(failures, now)
		// Trim failures outside the window.
		cutoff := now.Add(-policy.Window)
		for len(failures) > 0 && failures[0].Before(cutoff) {
			failures = failures[1:]
		}

		s.touch(j.Name, func(st *JobStatus) {
			st.Restarts++
			st.LastError = err
			st.LastErrorAt = now
		})
		s.Logger.Warn("supervisor: job failed",
			"job", j.Name,
			"err", err,
			"failures_in_window", len(failures),
			"critical", j.Critical,
		)

		if len(failures) > policy.Tries {
			if j.Critical {
				// Propagating cancels the errgroup, taking down siblings.
				return fmt.Errorf("supervisor: critical job %q exhausted retries: %w", j.Name, err)
			}
			s.Logger.Error("supervisor: non-critical job giving up",
				"job", j.Name, "err", err)
			return nil
		}

		// Sleep with backoff before retrying.
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(sleep):
		}
		sleep = time.Duration(float64(sleep) * policy.Multiplier)
		if sleep > policy.Max {
			sleep = policy.Max
		}
	}
}

// invoke calls j.Fn with panic recovery.
func (s *Supervisor) invoke(ctx context.Context, j Job) (err error) {
	defer func() {
		if r := recover(); r != nil {
			if s.Metrics != nil {
				s.Metrics.SupervisorPanic.WithLabelValues(j.Name).Inc()
			}
			stack := debug.Stack()
			s.Logger.Error("supervisor: panic recovered",
				"job", j.Name,
				"panic", r,
				"stack", string(stack),
			)
			err = fmt.Errorf("supervisor: panic in %q: %v", j.Name, r)
		}
	}()
	return j.Fn(ctx)
}

// touch mutates (or creates) the status entry for name under the supervisor's
// lock. The mutator is called with a non-nil *JobStatus.
func (s *Supervisor) touch(name string, mutate func(*JobStatus)) {
	if s == nil || name == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.statuses == nil {
		s.statuses = map[string]*JobStatus{}
	}
	st, ok := s.statuses[name]
	if !ok {
		st = &JobStatus{Name: name}
		s.statuses[name] = st
	}
	mutate(st)
}
