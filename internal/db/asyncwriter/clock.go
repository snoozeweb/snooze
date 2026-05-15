package asyncwriter

import (
	"sync"
	"time"
)

// Clock abstracts wall-clock and timer dependencies. Production uses
// SystemClock; tests inject MockClock for determinism.
type Clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
}

// SystemClock is the production Clock backed by stdlib time.
type SystemClock struct{}

// Now returns the current wall-clock time.
func (SystemClock) Now() time.Time { return time.Now() }

// After returns a channel that fires after duration d.
func (SystemClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// MockClock is a deterministic in-memory clock for tests. Time advances only
// when Advance is called.
type MockClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []mockTimer
}

type mockTimer struct {
	when time.Time
	ch   chan time.Time
}

// NewMockClock returns a clock initialised at zero time.
func NewMockClock(now time.Time) *MockClock {
	return &MockClock{now: now}
}

// Now returns the current simulated time.
func (m *MockClock) Now() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.now
}

// After returns a channel that fires when the clock is advanced past d.
func (m *MockClock) After(d time.Duration) <-chan time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	ch := make(chan time.Time, 1)
	m.timers = append(m.timers, mockTimer{when: m.now.Add(d), ch: ch})
	return ch
}

// Advance moves the clock forward by d and fires any timers whose deadline
// has been reached.
func (m *MockClock) Advance(d time.Duration) {
	m.mu.Lock()
	m.now = m.now.Add(d)
	now := m.now
	remaining := m.timers[:0]
	fired := []mockTimer{}
	for _, t := range m.timers {
		if !t.when.After(now) {
			fired = append(fired, t)
		} else {
			remaining = append(remaining, t)
		}
	}
	m.timers = remaining
	m.mu.Unlock()
	for _, t := range fired {
		t.ch <- now
		close(t.ch)
	}
}
