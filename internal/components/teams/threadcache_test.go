package teams

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestThreadCache_PutAndGet(t *testing.T) {
	c := newThreadCache(0)
	c.Put("teams/T/channels/C", "root-1", "uid-1")
	require.Equal(t, "uid-1", c.Get("teams/T/channels/C", "root-1"))
	require.Equal(t, "", c.Get("teams/T/channels/C", "root-2"))
	require.Equal(t, "", c.Get("teams/T/channels/OTHER", "root-1"))
}

// TestThreadCache_OverwriteReturnsLatest covers re-keyed puts: if the
// bridge ever sees the same (channel, root) twice (e.g. an /alert retry),
// the cache must return the most-recent record_uid, not the first.
func TestThreadCache_OverwriteReturnsLatest(t *testing.T) {
	c := newThreadCache(0)
	c.Put("teams/T/channels/C", "root-1", "uid-old")
	c.Put("teams/T/channels/C", "root-1", "uid-new")
	require.Equal(t, "uid-new", c.Get("teams/T/channels/C", "root-1"))
	require.Equal(t, 1, c.Len(), "overwrite must not grow the cache")
}

// TestThreadCache_LRUEvictionOldest is the bounded-memory guarantee:
// once the cache is full, the LEAST recently accessed entry is the first
// to drop. A Get on an old entry must refresh its recency so it survives
// subsequent evictions.
func TestThreadCache_LRUEvictionOldest(t *testing.T) {
	c := newThreadCache(3)
	for i := 0; i < 3; i++ {
		c.Put("teams/T/channels/C", "root-"+strconv.Itoa(i), "uid-"+strconv.Itoa(i))
	}
	// Touch root-0 so it becomes most-recent; root-1 should now be the LRU.
	require.Equal(t, "uid-0", c.Get("teams/T/channels/C", "root-0"))
	c.Put("teams/T/channels/C", "root-3", "uid-3")

	require.Equal(t, "uid-0", c.Get("teams/T/channels/C", "root-0"))
	require.Equal(t, "", c.Get("teams/T/channels/C", "root-1"),
		"least-recently-used entry must be evicted first")
	require.Equal(t, "uid-2", c.Get("teams/T/channels/C", "root-2"))
	require.Equal(t, "uid-3", c.Get("teams/T/channels/C", "root-3"))
	require.Equal(t, 3, c.Len())
}

// TestThreadCache_IgnoresEmptyInputs guards the bridge against caching
// pollution from a malformed /alert payload (channel "" or uid "" would
// turn the cache into a poison source for command dispatch).
func TestThreadCache_IgnoresEmptyInputs(t *testing.T) {
	c := newThreadCache(0)
	c.Put("", "root", "uid")
	c.Put("ch", "", "uid")
	c.Put("ch", "root", "")
	require.Equal(t, 0, c.Len())
	require.Equal(t, "", c.Get("", "root"))
	require.Equal(t, "", c.Get("ch", ""))
}
