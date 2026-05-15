package googlechat

import (
	"context"
)

// EventHandler is the callback invoked by the Pub/Sub loop for every decoded
// Chat event. Returning a non-nil error nacks the message; a nil return acks.
type EventHandler func(ctx context.Context, ev ChatEvent) error
