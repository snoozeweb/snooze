package teams

// threadcache.go owns the in-memory map of (channel, thread_root_id) →
// record_uid that powers chat-driven actions (ack / close / open / esc /
// snooze / comment from a Teams thread reply).
//
// The cache is populated by listener.handleAlert after a successful Graph
// sendMessage — at that point the bridge knows BOTH the originating record
// (carried in the /alert request payload) and the Graph message id that
// became the thread root. A subsequent reply in the same thread is
// resolved O(1) back to the record_uid by the poller.
//
// State is intentionally process-local: a daemon restart wipes the cache.
// Subsequent chat commands for threads created before the restart return
// a "thread not recognised" error to the user. The trade-off is no
// new schema field on the record collection and no extra DB round-trips
// in the hot path; the operator can always re-fire the alert (or use the
// snooze web) to re-populate.

import (
	"container/list"
	"sync"
)

// defaultThreadCacheSize is the max number of (channel, thread) entries
// retained at once. A typical noisy channel sees ~10² active threads;
// 4096 leaves headroom for several days of activity before LRU eviction
// kicks in. The bound matters because the cache lives in memory forever —
// without it, a long-running daemon would accumulate state indefinitely.
const defaultThreadCacheSize = 4096

// threadCache is a bounded LRU keyed on (channel, thread_root_id). It is
// safe for concurrent use across the listener (writes) and poller (reads).
//
// We use container/list under a mutex rather than a sync.Map because LRU
// eviction is the whole point — sync.Map gives us O(1) reads but no eviction
// policy.
type threadCache struct {
	mu    sync.Mutex
	max   int
	order *list.List               // most-recent at front; oldest at back
	index map[string]*list.Element // key → list element
}

// threadEntry is the value stored in the LRU. It carries the record uid
// (the actual action target) plus the inbound key so eviction can clear
// the index without a second lookup.
type threadEntry struct {
	key       string
	recordUID string
}

// newThreadCache returns an empty cache with the supplied capacity. A
// non-positive capacity falls back to defaultThreadCacheSize so callers
// can pass 0 to mean "use the default".
func newThreadCache(capacity int) *threadCache {
	if capacity <= 0 {
		capacity = defaultThreadCacheSize
	}
	return &threadCache{
		max:   capacity,
		order: list.New(),
		index: make(map[string]*list.Element, capacity),
	}
}

// threadKey composes the cache key from the (channel, thread_root_id)
// pair. The format is opaque to callers — never parse it back.
func threadKey(channel, thread string) string {
	return channel + "|" + thread
}

// Put inserts (channel, thread) → recordUID. An existing entry for the
// same key is updated in place and moved to the MRU position. When the
// cache is at capacity, the LRU entry is evicted before the insert.
//
// Empty channel, thread, or recordUID are silently ignored — they would
// poison the cache and the bridge has no useful value to return for them.
func (c *threadCache) Put(channel, thread, recordUID string) {
	if channel == "" || thread == "" || recordUID == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	key := threadKey(channel, thread)
	if el, ok := c.index[key]; ok {
		el.Value.(*threadEntry).recordUID = recordUID
		c.order.MoveToFront(el)
		return
	}
	if c.order.Len() >= c.max {
		oldest := c.order.Back()
		if oldest != nil {
			ent := oldest.Value.(*threadEntry)
			delete(c.index, ent.key)
			c.order.Remove(oldest)
		}
	}
	el := c.order.PushFront(&threadEntry{key: key, recordUID: recordUID})
	c.index[key] = el
}

// Get returns the record uid for (channel, thread), or "" if absent. A
// hit moves the entry to the MRU position so the cache evicts cold
// entries first.
func (c *threadCache) Get(channel, thread string) string {
	if channel == "" || thread == "" {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.index[threadKey(channel, thread)]
	if !ok {
		return ""
	}
	c.order.MoveToFront(el)
	return el.Value.(*threadEntry).recordUID
}

// Len returns the number of entries currently held. Used by tests.
func (c *threadCache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.order.Len()
}
