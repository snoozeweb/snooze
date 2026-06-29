package auth

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestLoopChainRoundTrip verifies WithLoopChain / LoopChainFrom round-trip a
// server-id chain through the context, and that a context with no chain yields
// nil (the federation Processor relies on a missing chain reading as empty).
func TestLoopChainRoundTrip(t *testing.T) {
	t.Parallel()

	chain := []string{"a", "b"}
	ctx := WithLoopChain(context.Background(), chain)
	got := LoopChainFrom(ctx)
	require.Equal(t, chain, got)
}

func TestLoopChainMissing(t *testing.T) {
	t.Parallel()
	require.Nil(t, LoopChainFrom(context.Background()))
}

func TestLoopChainEmpty(t *testing.T) {
	t.Parallel()
	ctx := WithLoopChain(context.Background(), nil)
	require.Empty(t, LoopChainFrom(ctx))
}
