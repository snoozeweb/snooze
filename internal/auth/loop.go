package auth

import "context"

// loopChainKey is the unexported key type for the relay loop chain. The
// unexported nature prevents accidental collisions with other packages keying
// off context.Value.
type loopChainKey struct{}

// WithLoopChain returns a derived context carrying the X-Snooze-Loop chain of
// server ids a relayed alert has already traversed. The ingestion handler is
// the canonical caller: it parses the inbound X-Snooze-Loop header into a slice
// and stashes it here so the forward federation Processor can read it.
func WithLoopChain(ctx context.Context, chain []string) context.Context {
	return context.WithValue(ctx, loopChainKey{}, chain)
}

// LoopChainFrom returns the loop chain previously attached by WithLoopChain, or
// nil when none is present (a directly-ingested, non-relayed alert).
func LoopChainFrom(ctx context.Context) []string {
	v, _ := ctx.Value(loopChainKey{}).([]string)
	return v
}
