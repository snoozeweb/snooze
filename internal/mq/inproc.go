package mq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// InprocConfig controls the inproc Bus capacity.
type InprocConfig struct {
	// QueueBuffer caps the number of buffered messages per queue. Defaults
	// to 1024. Publish drops new messages when the buffer is full so the
	// caller is never blocked.
	QueueBuffer int
}

// inprocBus is the buffered-channel-backed default Bus, suitable for
// single-instance Snooze deployments and unit tests.
type inprocBus struct {
	cfg InprocConfig

	mu      sync.Mutex
	queues  map[string]chan Message
	wg      sync.WaitGroup
	closed  bool
	rootCtx context.Context
	cancel  context.CancelFunc
}

// NewInproc constructs an in-process Bus.
func NewInproc(cfg InprocConfig) Bus {
	if cfg.QueueBuffer <= 0 {
		cfg.QueueBuffer = 1024
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &inprocBus{
		cfg:     cfg,
		queues:  make(map[string]chan Message),
		rootCtx: ctx,
		cancel:  cancel,
	}
}

// queueLocked returns the channel for queue, creating it on demand.
// Caller must hold b.mu.
func (b *inprocBus) queueLocked(queue string) chan Message {
	if ch, ok := b.queues[queue]; ok {
		return ch
	}
	ch := make(chan Message, b.cfg.QueueBuffer)
	b.queues[queue] = ch
	return ch
}

// Publish enqueues payload on queue with a non-blocking send.
func (b *inprocBus) Publish(_ context.Context, queue string, payload any) error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return errors.New("mq: inproc bus closed")
	}
	ch := b.queueLocked(queue)
	b.mu.Unlock()

	enc, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("mq inproc: marshal: %w", err)
	}
	msg := Message{
		ID:        uuid.NewString(),
		Queue:     queue,
		Payload:   enc,
		Timestamp: time.Now().UTC(),
	}
	select {
	case ch <- msg:
		return nil
	default:
		return fmt.Errorf("mq inproc: queue %q full", queue)
	}
}

// Subscribe launches Concurrency workers consuming from queue.
func (b *inprocBus) Subscribe(ctx context.Context, queue string, opts SubscribeOpts, h Handler) error {
	if h == nil {
		return errors.New("mq inproc: nil handler")
	}
	opts = defaults(opts)

	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return errors.New("mq: inproc bus closed")
	}
	ch := b.queueLocked(queue)
	b.mu.Unlock()

	// Run workers tied to the longer of the caller ctx and the bus
	// lifetime so Close() also stops them.
	workerCtx, cancel := context.WithCancel(ctx)
	go func() {
		select {
		case <-b.rootCtx.Done():
			cancel()
		case <-workerCtx.Done():
		}
	}()

	for i := 0; i < opts.Concurrency; i++ {
		b.wg.Add(1)
		go b.runWorker(workerCtx, ch, opts, h)
	}
	return nil
}

// runWorker reads from ch, batches up to opts.BatchSize or BatchTimer, and
// dispatches each message through h individually.
func (b *inprocBus) runWorker(ctx context.Context, ch <-chan Message, opts SubscribeOpts, h Handler) {
	defer b.wg.Done()
	batch := make([]Message, 0, opts.BatchSize)
	timer := time.NewTimer(opts.BatchTimer)
	defer timer.Stop()

	flush := func() {
		for _, m := range batch {
			// One Handler call per message — batching only controls how
			// many we claim in a single poll, not how we deliver them.
			if err := h(ctx, m); err != nil {
				// Best-effort retry: re-enqueue is not safe (the channel
				// may be full); the inproc bus uses at-most-once delivery.
				// Errors are silently dropped here because logging is the
				// handler's responsibility.
				_ = err
			}
		}
		batch = batch[:0]
	}

	for {
		// Reset timer for the next idle window.
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(opts.BatchTimer)

		select {
		case <-ctx.Done():
			flush()
			return
		case m, ok := <-ch:
			if !ok {
				flush()
				return
			}
			batch = append(batch, m)
			// Drain up to BatchSize without blocking.
			for len(batch) < opts.BatchSize {
				select {
				case m2, ok := <-ch:
					if !ok {
						flush()
						return
					}
					batch = append(batch, m2)
				default:
					goto dispatch
				}
			}
		dispatch:
			flush()
		case <-timer.C:
			// Idle tick: nothing to flush, just loop.
		}
	}
}

// Close cancels the root context, drains pending Subscribe workers, and
// prevents further Publish/Subscribe. Idempotent.
func (b *inprocBus) Close() error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.closed = true
	// Close every queue so workers reading on it observe end-of-stream
	// and exit cleanly.
	for _, ch := range b.queues {
		close(ch)
	}
	b.queues = nil
	b.mu.Unlock()
	b.cancel()
	b.wg.Wait()
	return nil
}
