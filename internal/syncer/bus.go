// Package syncer coordinates plugin reloads across Snooze instances using a
// pluggable event bus (Postgres LISTEN/NOTIFY, Mongo change streams, or
// in-process channels for SQLite).
package syncer

import (
	"context"
	"time"
)

// Event is the unit of cross-instance change notification.
type Event struct {
	Topic      string    // e.g. "collection.rule.acme" or "plugin.rule"
	Op         string    // "write" | "delete" | "replace" | "reload"
	Collection string    // empty for plugin-level events
	Tenant     string    // NEW — tenant slug for tenant-scoped events; "" for global
	UIDs       []string  // optional, may be empty
	At         time.Time // wall-clock when the publisher emitted the event
}

// Bus is the cross-instance event broadcaster. Implementations: in-process
// channels (SQLite), Postgres LISTEN/NOTIFY, Mongo change streams.
type Bus interface {
	// Publish emits an event. Implementations should be non-blocking up to a
	// reasonable backpressure threshold.
	Publish(ctx context.Context, e Event) error
	// Subscribe returns a channel that receives every event whose Topic matches
	// the prefix. The channel is closed when ctx is cancelled.
	Subscribe(ctx context.Context, topicPrefix string) (<-chan Event, error)
	// Close releases all resources. Idempotent.
	Close() error
}

// CollectionTopic builds the canonical topic string for a collection event.
// For tenant-scoped events, the topic is "collection.<collection>.<tenant>".
// For global collections (tenant == ""), the topic is "collection.<collection>".
// Syncer subscriptions use the prefix "collection.<collection>" which matches
// both forms because Subscribe uses HasPrefix semantics.
func CollectionTopic(collection, tenant string) string {
	if tenant == "" {
		return "collection." + collection
	}
	return "collection." + collection + "." + tenant
}
