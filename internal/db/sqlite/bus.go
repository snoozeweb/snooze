// In-process event bus for the SQLite backend.
//
// SQLite is a single-instance store: every subscriber lives in the same
// process as the publisher, so a channel fan-out suffices. The bus is
// non-blocking — slow subscribers get a buffered channel sized for short
// stalls; on overflow the event is dropped silently rather than back-
// pressuring the writer. The intent matches the Postgres/Mongo buses:
// "deliver if you can, but never wedge a mutation".

package sqlite

import (
	"context"
	"strings"
	"sync"

	"github.com/snoozeweb/snooze/internal/syncer"
)

// inprocBus is a buffered-channel fan-out used as the SQLite syncer.Bus.
type inprocBus struct {
	mu     sync.Mutex
	subs   []*subscription
	closed bool
}

type subscription struct {
	prefix string
	ch     chan syncer.Event
	ctx    context.Context
	once   sync.Once
}

const subscriberBuffer = 64

func newInprocBus() *inprocBus { return &inprocBus{} }

// Publish fans an event out to every subscriber whose prefix matches the
// event topic. Slow subscribers drop the event rather than block the
// mutation path.
func (b *inprocBus) Publish(_ context.Context, e syncer.Event) error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	// Snapshot subscribers so we don't hold the lock during sends.
	subs := make([]*subscription, len(b.subs))
	copy(subs, b.subs)
	b.mu.Unlock()
	for _, s := range subs {
		if s.prefix != "" && !strings.HasPrefix(e.Topic, s.prefix) {
			continue
		}
		select {
		case s.ch <- e:
		default:
			// Buffer full; drop.
		}
	}
	return nil
}

// Subscribe returns a channel that receives events whose Topic starts with
// topicPrefix. The channel is closed when ctx is cancelled or Close is
// called on the bus.
func (b *inprocBus) Subscribe(ctx context.Context, topicPrefix string) (<-chan syncer.Event, error) {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		ch := make(chan syncer.Event)
		close(ch)
		return ch, nil
	}
	s := &subscription{
		prefix: topicPrefix,
		ch:     make(chan syncer.Event, subscriberBuffer),
		ctx:    ctx,
	}
	b.subs = append(b.subs, s)
	b.mu.Unlock()
	go func() {
		<-ctx.Done()
		b.unsubscribe(s)
	}()
	return s.ch, nil
}

// unsubscribe removes s from the subscriber list and closes its channel.
// Safe to call multiple times.
func (b *inprocBus) unsubscribe(s *subscription) {
	b.mu.Lock()
	for i, cur := range b.subs {
		if cur == s {
			b.subs = append(b.subs[:i], b.subs[i+1:]...)
			break
		}
	}
	b.mu.Unlock()
	s.once.Do(func() { close(s.ch) })
}

// Close drops every subscriber and rejects future Publish/Subscribe calls.
// Idempotent.
func (b *inprocBus) Close() error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.closed = true
	subs := b.subs
	b.subs = nil
	b.mu.Unlock()
	for _, s := range subs {
		s.once.Do(func() { close(s.ch) })
	}
	return nil
}
