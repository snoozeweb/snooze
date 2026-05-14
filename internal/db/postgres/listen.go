package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/japannext/snooze/internal/syncer"
)

// notifyChannel is the single Postgres LISTEN channel every Snooze instance
// joins. Payloads carry the collection, op, and affected uids as JSON.
const notifyChannel = "snooze_changes"

// notifyPayload is the JSON shape published with pg_notify. Keep
// backward-compatible: new fields go at the end with omitempty.
type notifyPayload struct {
	Collection string   `json:"collection"`
	Op         string   `json:"op"`
	UIDs       []string `json:"uids,omitempty"`
}

// pgBus is the Postgres LISTEN/NOTIFY-backed implementation of syncer.Bus.
type pgBus struct {
	cfg Config

	mu          sync.RWMutex
	subscribers []*subscription
	closed      bool

	// publisher pool is used to issue pg_notify; the listener uses its own
	// dedicated connection acquired and hijacked from the pool.
	pool *pgxpool.Pool

	stopOnce sync.Once
	stopCh   chan struct{}
	doneCh   chan struct{}
}

// subscription tracks a single Subscribe caller.
type subscription struct {
	prefix string
	ch     chan syncer.Event
	ctx    context.Context
}

// newPgBus constructs the bus and starts the background listener goroutine.
func newPgBus(parentCtx context.Context, pool *pgxpool.Pool, cfg Config) (*pgBus, error) {
	b := &pgBus{
		cfg:    cfg,
		pool:   pool,
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
	}
	// Spin up the listener loop. parentCtx is the lifetime of the driver;
	// listener exits when ctx is cancelled or Close() is called.
	go b.listenLoop(parentCtx)
	return b, nil
}

// Publish broadcasts an event to every Snooze instance via pg_notify and
// fans it out to local subscribers (so single-instance setups work without
// needing to round-trip the database).
func (b *pgBus) Publish(ctx context.Context, e syncer.Event) error {
	b.mu.RLock()
	closed := b.closed
	b.mu.RUnlock()
	if closed {
		return errors.New("postgres bus: closed")
	}
	payload := notifyPayload{
		Collection: e.Collection,
		Op:         e.Op,
		UIDs:       e.UIDs,
	}
	enc, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("postgres bus: marshal: %w", err)
	}
	if _, err := b.pool.Exec(ctx, "SELECT pg_notify($1, $2)", notifyChannel, string(enc)); err != nil {
		return fmt.Errorf("postgres bus: pg_notify: %w", err)
	}
	// Also fan out locally — pg_notify won't deliver to the issuing
	// connection if everything is in the same process, and we want the
	// in-process subscribers to see their own writes.
	b.deliverLocal(e)
	return nil
}

// Subscribe registers a topic-prefix subscription. The returned channel is
// closed when ctx is cancelled or the bus is closed.
func (b *pgBus) Subscribe(ctx context.Context, topicPrefix string) (<-chan syncer.Event, error) {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil, errors.New("postgres bus: closed")
	}
	sub := &subscription{
		prefix: topicPrefix,
		ch:     make(chan syncer.Event, 32),
		ctx:    ctx,
	}
	b.subscribers = append(b.subscribers, sub)
	b.mu.Unlock()
	go func() {
		<-ctx.Done()
		b.removeSubscription(sub)
		close(sub.ch)
	}()
	return sub.ch, nil
}

// Close shuts down the listener and closes every subscriber channel.
// Idempotent.
func (b *pgBus) Close() error {
	b.stopOnce.Do(func() {
		b.mu.Lock()
		b.closed = true
		b.mu.Unlock()
		close(b.stopCh)
	})
	// Wait for the listen loop to release the dedicated conn so callers
	// can't observe a half-open state. Bounded so we don't hang at
	// shutdown if the loop is wedged on the network.
	select {
	case <-b.doneCh:
	case <-time.After(5 * time.Second):
	}
	return nil
}

func (b *pgBus) removeSubscription(target *subscription) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i, s := range b.subscribers {
		if s == target {
			b.subscribers = append(b.subscribers[:i], b.subscribers[i+1:]...)
			return
		}
	}
}

// deliverLocal fans an event out to every matching subscriber. Drops events
// on full channels rather than blocking — the contract is "non-blocking up
// to a reasonable backpressure threshold" (syncer.Bus.Publish).
func (b *pgBus) deliverLocal(e syncer.Event) {
	b.mu.RLock()
	subs := append([]*subscription(nil), b.subscribers...)
	b.mu.RUnlock()
	for _, s := range subs {
		if s.prefix != "" && !strings.HasPrefix(e.Topic, s.prefix) {
			continue
		}
		select {
		case s.ch <- e:
		default:
			// Drop: subscriber too slow.
		}
	}
}

// listenLoop runs forever, reconnecting on failure with exponential backoff.
// Each cycle acquires a dedicated conn from the pool, hijacks it (so it
// stops being recycled), and parks on WaitForNotification.
func (b *pgBus) listenLoop(parentCtx context.Context) {
	defer close(b.doneCh)
	backoff := 200 * time.Millisecond
	for {
		select {
		case <-b.stopCh:
			return
		case <-parentCtx.Done():
			return
		default:
		}

		if err := b.runListen(parentCtx); err != nil {
			// Sleep with backoff before reconnecting.
			select {
			case <-b.stopCh:
				return
			case <-parentCtx.Done():
				return
			case <-time.After(backoff):
			}
			if backoff < 5*time.Second {
				backoff *= 2
			}
			continue
		}
		backoff = 200 * time.Millisecond
	}
}

// runListen acquires one dedicated connection, hijacks it, and forwards
// every notification it receives until the connection drops or stopCh
// fires. Returns nil if exit was clean, an error otherwise.
func (b *pgBus) runListen(parentCtx context.Context) error {
	conn, err := b.pool.Acquire(parentCtx)
	if err != nil {
		return fmt.Errorf("postgres bus: acquire: %w", err)
	}
	// Hijack so the pool stops trying to recycle this conn.
	hijacked := conn.Hijack()
	defer hijacked.Close(parentCtx) //nolint:errcheck

	if _, err := hijacked.Exec(parentCtx, "LISTEN "+quoteIdent(notifyChannel)); err != nil {
		return fmt.Errorf("postgres bus: LISTEN: %w", err)
	}

	// Stop channel honoured via a child context cancelled on stopCh.
	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()
	go func() {
		select {
		case <-b.stopCh:
			cancel()
		case <-ctx.Done():
		}
	}()

	for {
		notif, err := hijacked.WaitForNotification(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("postgres bus: wait: %w", err)
		}
		var p notifyPayload
		if err := json.Unmarshal([]byte(notif.Payload), &p); err != nil {
			// Malformed payload — skip but keep listening.
			continue
		}
		event := syncer.Event{
			Topic:      "collection." + p.Collection,
			Op:         p.Op,
			Collection: p.Collection,
			UIDs:       p.UIDs,
			At:         time.Now(),
		}
		b.deliverLocal(event)
	}
}

// notifyTx writes a pg_notify inside the supplied transaction so the
// notification ships only on commit. Callers running outside a transaction
// can use the pgBus.Publish path instead.
func notifyTx(ctx context.Context, tx pgx.Tx, collection, op string, uids []string) error {
	payload := notifyPayload{Collection: collection, Op: op, UIDs: uids}
	enc, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("postgres notify: marshal: %w", err)
	}
	if _, err := tx.Exec(ctx, "SELECT pg_notify($1, $2)", notifyChannel, string(enc)); err != nil {
		return fmt.Errorf("postgres notify: exec: %w", err)
	}
	return nil
}

// notifyExec writes a pg_notify directly via the pool (no enclosing tx).
func notifyExec(ctx context.Context, pool *pgxpool.Pool, collection, op string, uids []string) error {
	payload := notifyPayload{Collection: collection, Op: op, UIDs: uids}
	enc, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("postgres notify: marshal: %w", err)
	}
	if _, err := pool.Exec(ctx, "SELECT pg_notify($1, $2)", notifyChannel, string(enc)); err != nil {
		return fmt.Errorf("postgres notify: exec: %w", err)
	}
	return nil
}
