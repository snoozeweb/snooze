package plugins

import (
	"sync"
	"sync/atomic"
)

// Cache is a generic, copy-on-replace, race-free snapshot store used by plugins
// to keep their working set in memory. Readers obtain a shallow copy of the
// current slice; writers atomically install a new generation.
//
// Cache is safe for concurrent use. The zero value is a valid, empty cache.
type Cache[T any] struct {
	mu   sync.RWMutex
	data []T
	ver  atomic.Int64
}

// Snapshot returns a shallow copy of the current data slice. The caller may
// mutate the returned slice's element values but the backing array is
// independent of any concurrent Replace.
func (c *Cache[T]) Snapshot() []T {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if len(c.data) == 0 {
		return nil
	}
	out := make([]T, len(c.data))
	copy(out, c.data)
	return out
}

// Replace installs items as the new authoritative slice and bumps the
// generation counter. The slice is taken by reference, so callers must not
// continue to mutate it after the call returns.
func (c *Cache[T]) Replace(items []T) {
	c.mu.Lock()
	c.data = items
	c.mu.Unlock()
	c.ver.Add(1)
}

// Version returns the monotonically increasing generation counter. Useful for
// cheap cache-invalidation checks (e.g. did anything change since I last read?).
func (c *Cache[T]) Version() int64 {
	return c.ver.Load()
}
