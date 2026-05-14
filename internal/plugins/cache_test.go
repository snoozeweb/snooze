package plugins

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCache_ZeroValue(t *testing.T) {
	t.Parallel()
	var c Cache[int]
	require.Equal(t, int64(0), c.Version())
	require.Nil(t, c.Snapshot())
}

func TestCache_ReplaceBumpsVersion(t *testing.T) {
	t.Parallel()
	var c Cache[string]
	require.Equal(t, int64(0), c.Version())
	c.Replace([]string{"a", "b"})
	require.Equal(t, int64(1), c.Version())
	c.Replace([]string{"c"})
	require.Equal(t, int64(2), c.Version())
	require.Equal(t, []string{"c"}, c.Snapshot())
}

func TestCache_SnapshotIsIndependent(t *testing.T) {
	t.Parallel()
	var c Cache[int]
	c.Replace([]int{1, 2, 3})
	snap := c.Snapshot()
	snap[0] = 99
	// A fresh snapshot must not see the mutation.
	require.Equal(t, []int{1, 2, 3}, c.Snapshot())
}

func TestCache_ConcurrentReadersWriters(t *testing.T) {
	t.Parallel()
	var c Cache[int]
	c.Replace([]int{0})

	const writers = 4
	const readers = 8
	const iters = 1_000

	var wg sync.WaitGroup
	var reads atomic.Int64

	// Writers swap in fresh slices.
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(seed int) {
			defer wg.Done()
			for i := 0; i < iters; i++ {
				c.Replace([]int{seed, i})
			}
		}(w)
	}

	// Readers spin on Snapshot() — exercised under `-race`.
	for r := 0; r < readers; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iters; i++ {
				snap := c.Snapshot()
				if len(snap) > 0 {
					reads.Add(1)
				}
			}
		}()
	}

	wg.Wait()
	require.Greater(t, c.Version(), int64(0))
	require.Greater(t, reads.Load(), int64(0))
}
