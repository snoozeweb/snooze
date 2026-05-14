package housekeeper

import (
	"context"
	"time"
)

// JobFunc adapts a bare func to the Job interface. The Name is taken at
// construction time.
type JobFunc struct {
	name string
	fn   func(ctx context.Context) error
}

// NewJobFunc wraps fn into a named Job.
func NewJobFunc(name string, fn func(ctx context.Context) error) JobFunc {
	return JobFunc{name: name, fn: fn}
}

// Name returns the job's display name.
func (j JobFunc) Name() string { return j.name }

// Run invokes the wrapped function.
func (j JobFunc) Run(ctx context.Context) error { return j.fn(ctx) }

// IntervalJob is a Job + its desired Interval, returned by the default-job
// factories so callers can both Register and inspect the schedule.
type IntervalJob struct {
	Job      Job
	Interval time.Duration
}

// Schedule returns the matching Schedule value.
func (i IntervalJob) Schedule() Schedule { return Schedule{Interval: i.Interval} }

// CronJob is a Job + its cron expression, returned by the default-job
// factories that run on a daily cadence.
type CronJob struct {
	Job  Job
	Cron string
}

// Schedule returns the matching Schedule value.
func (c CronJob) Schedule() Schedule { return Schedule{Cron: c.Cron} }
