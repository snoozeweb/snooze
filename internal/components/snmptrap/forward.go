package snmptrap

import (
	"context"
	"log/slog"
	"time"

	"github.com/snoozeweb/snooze/pkg/snoozeclient"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// alertPoster is the subset of snoozeclient.Client used by the forwarder.
// Defined as an interface so tests can drop in a stub.
type alertPoster interface {
	PostAlert(ctx context.Context, rec snoozetypes.Record) (snoozetypes.Record, error)
}

// clock abstracts wall-clock reads. Tests override this with a frozen value.
type clock func() time.Time

// Forwarder POSTs parsed traps to the Snooze API. It owns no goroutines —
// callers drive it from their own worker pool.
type Forwarder struct {
	client alertPoster
	logger *slog.Logger
	now    clock
}

// NewForwarder wires a Forwarder around a snoozeclient.Client. logger may be
// nil (defaults to slog.Default()).
func NewForwarder(client *snoozeclient.Client, logger *slog.Logger) *Forwarder {
	if logger == nil {
		logger = slog.Default()
	}
	return &Forwarder{client: client, logger: logger, now: time.Now}
}

// Forward turns a ParsedTrap into a Record and POSTs it. The returned error
// is the raw snoozeclient error (typed *APIError for non-2xx), so callers can
// branch on Status when implementing retry / dead-lettering.
func (f *Forwarder) Forward(ctx context.Context, p ParsedTrap) error {
	rec := p.ToRecord(f.now())
	_, err := f.client.PostAlert(ctx, rec)
	if err != nil {
		f.logger.Warn("snmptrap: forward failed",
			slog.String("host", rec.Host),
			slog.String("process", rec.Process),
			slog.Any("err", err),
		)
		return err
	}
	f.logger.Debug("snmptrap: forwarded",
		slog.String("host", rec.Host),
		slog.String("process", rec.Process),
	)
	return nil
}
