// Package mq is the pluggable message-queue layer intended to back Snooze's
// async pipelines (notifications, webhook fan-out). It abstracts the queue
// transport so the choice of DB does not lock the deployment into one
// queue technology; backends are selected via Config.Kind on Manager.
//
// NOTE: this layer is not yet wired into a pipeline — nothing publishes to or
// subscribes from the bus today. It is constructed at boot and closed on
// shutdown (see core.Core.Close), but carries no traffic until a producer and
// consumer are added.
package mq

import (
	"context"
	"time"
)

// Message is the unit of work flowing through any mq.Bus implementation.
type Message struct {
	// ID is the queue-assigned identifier; unique per backend.
	ID string
	// Queue names the logical destination this message was published to.
	Queue string
	// Payload is the opaque JSON-encoded body.
	Payload []byte
	// Headers carries optional metadata (correlation ids, attempt counts).
	Headers map[string]string
	// Timestamp is the publish time as recorded by the backend.
	Timestamp time.Time
}

// Handler processes a single message. Returning nil acknowledges the
// message; a non-nil error leaves it on the queue for retry per the
// backend's at-least-once semantics.
type Handler func(ctx context.Context, msg Message) error

// SubscribeOpts controls consumer concurrency and batching. Mirrors the
// Python mq.AsyncQueue knobs (`maxsize` → BatchSize, `timer` → BatchTimer).
type SubscribeOpts struct {
	// BatchSize is the maximum number of messages claimed per batch poll.
	// Defaults to 1 when zero.
	BatchSize int
	// BatchTimer caps the wait between batch polls in absence of new
	// notifications. Defaults to 1s when zero.
	BatchTimer time.Duration
	// Concurrency is the number of worker goroutines per Subscribe call.
	// Defaults to 1 when zero.
	Concurrency int
}

// Bus is the queue contract every backend implements.
type Bus interface {
	// Publish encodes payload as JSON and enqueues it on queue.
	Publish(ctx context.Context, queue string, payload any) error
	// Subscribe spawns workers that invoke h for each message claimed from
	// queue until ctx is cancelled or Close is called. Subscribe returns
	// after the worker goroutines have started; errors arriving after that
	// are reported via the handler context.
	Subscribe(ctx context.Context, queue string, opts SubscribeOpts, h Handler) error
	// Close releases any resources the bus owns. Idempotent.
	Close() error
}

// defaults applies zero-value sensible defaults to opts.
func defaults(opts SubscribeOpts) SubscribeOpts {
	if opts.BatchSize <= 0 {
		opts.BatchSize = 1
	}
	if opts.BatchTimer <= 0 {
		opts.BatchTimer = time.Second
	}
	if opts.Concurrency <= 0 {
		opts.Concurrency = 1
	}
	return opts
}

// TenantQueue returns the canonical queue name for a given base name and tenant
// slug. The convention is "<base>.<tenant>" for tenant-scoped queues (e.g.
// "notifications.acme") and the plain base name for global or unscoped queues
// (tenant == "").
//
// This encoding keeps tenant traffic logically isolated on shared queue
// backends (Postgres, Mongo) without requiring separate tables or collections
// per tenant.
func TenantQueue(base, tenant string) string {
	if tenant == "" {
		return base
	}
	return base + "." + tenant
}
