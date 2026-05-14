package googlechat

import (
	"context"
	"fmt"
	"log/slog"

	"cloud.google.com/go/pubsub"
	"google.golang.org/api/option"
)

// EventHandler is the callback invoked by the Pub/Sub loop for every decoded
// Chat event. Returning a non-nil error nacks the message; a nil return acks.
type EventHandler func(ctx context.Context, ev ChatEvent) error

// pubsubReceiver wraps a Pub/Sub Subscription with the daemon's decode +
// dispatch logic. It is package-private — Daemon owns its lifecycle.
type pubsubReceiver struct {
	subscription *pubsub.Subscription
	handler      EventHandler
	logger       *slog.Logger
}

// newPubsubReceiver builds the Pub/Sub client + subscription. The client is
// returned alongside so the caller can defer Close on shutdown.
func newPubsubReceiver(ctx context.Context, cfg Config, handler EventHandler, logger *slog.Logger) (*pubsubReceiver, *pubsub.Client, error) {
	if handler == nil {
		return nil, nil, fmt.Errorf("googlechat: handler is required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	opts := []option.ClientOption{}
	if cfg.ServiceAccountKey != "" {
		opts = append(opts, option.WithCredentialsFile(cfg.ServiceAccountKey))
	}
	client, err := pubsub.NewClient(ctx, cfg.GCPProject, opts...)
	if err != nil {
		return nil, nil, fmt.Errorf("googlechat: pubsub client: %w", err)
	}
	sub := client.Subscription(cfg.PubSubSubscription)
	return &pubsubReceiver{subscription: sub, handler: handler, logger: logger}, client, nil
}

// run blocks consuming messages from the subscription until ctx is cancelled
// or the Pub/Sub client returns a terminal error. Each message is decoded,
// forwarded to the handler, and ack'd on success / nack'd on failure.
//
// The SDK manages its own goroutine pool; the callback we pass is invoked
// concurrently. Snoozeclient and the Forwarder are safe for concurrent use.
func (r *pubsubReceiver) run(ctx context.Context) error {
	if r == nil || r.subscription == nil {
		return fmt.Errorf("googlechat: receiver not initialised")
	}
	err := r.subscription.Receive(ctx, func(mctx context.Context, m *pubsub.Message) {
		ev, perr := parseEvent(m.Data)
		if perr != nil {
			r.logger.Warn("googlechat: invalid pubsub payload", slog.Any("err", perr))
			// Bad payload — ack to avoid an infinite redelivery storm. The
			// operator can grep logs to find it.
			m.Ack()
			return
		}
		if herr := r.handler(mctx, ev); herr != nil {
			r.logger.Warn("googlechat: handler error, will redeliver", slog.Any("err", herr))
			m.Nack()
			return
		}
		m.Ack()
	})
	if err != nil && ctx.Err() == nil {
		return fmt.Errorf("googlechat: pubsub receive: %w", err)
	}
	return nil
}
