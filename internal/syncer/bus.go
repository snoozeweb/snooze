// Package syncer coordinates plugin reloads across Snooze instances using a
// pluggable event bus (Postgres LISTEN/NOTIFY, Mongo change streams, or
// in-process channels for SQLite).
package syncer

import (
	"context"
	"strings"
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
// Syncer subscriptions use the prefix "collection.<collection>", which matches
// both forms because Subscribe matches on dot-delimited segments — see
// TopicMatches.
func CollectionTopic(collection, tenant string) string {
	if tenant == "" {
		return "collection." + collection
	}
	return "collection." + collection + "." + tenant
}

// TopicMatches reports whether topic is delivered to a subscription registered
// with topicPrefix. The match is delimiter-aware: a prefix matches the topic
// itself or any dot-delimited extension of it, never a longer sibling name.
//
//	TopicMatches("collection.notification", "collection.notification")         // true
//	TopicMatches("collection.notification.acme", "collection.notification")    // true
//	TopicMatches("collection.notificationlog.acme", "collection.notification") // false
//
// The last line is the whole point. Plain strings.HasPrefix made every write to
// the `notificationlog` collection reload the `notification` plugin's cache on
// every driver, and any future collection whose name extends another's
// (rule/rules, snooze/snoozed) would have inherited the same bug.
//
// An empty prefix is the wildcard subscription: it matches every topic, which
// is what the Postgres and SQLite buses already did (`s.prefix != "" && ...`)
// and what strings.HasPrefix(topic, "") gave the Mongo bus implicitly.
func TopicMatches(topic, topicPrefix string) bool {
	if topicPrefix == "" {
		return true
	}
	if topic == topicPrefix {
		return true
	}
	// A caller that spelled the separator itself already asked for a boundary
	// match; appending a second dot would make the prefix unmatchable.
	if strings.HasSuffix(topicPrefix, ".") {
		return strings.HasPrefix(topic, topicPrefix)
	}
	return strings.HasPrefix(topic, topicPrefix+".")
}
