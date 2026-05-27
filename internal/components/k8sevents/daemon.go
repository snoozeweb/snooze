package k8sevents

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/snoozeweb/snooze/pkg/snoozeclient"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// snoozePoster is the slice of snoozeclient.Client the daemon needs. Declaring
// it as an interface lets tests inject a fake poster without an HTTP round-trip
// (the real *snoozeclient.Client satisfies it).
type snoozePoster interface {
	PostAlert(ctx context.Context, rec snoozetypes.Record) (snoozetypes.Record, error)
}

// Daemon wires a Kubernetes Event watcher to a snoozeclient. cmd/snooze-k8s-events
// constructs one and calls Run.
type Daemon struct {
	cfg    Config
	logger *slog.Logger

	snooze  *snoozeclient.Client
	poster  snoozePoster
	watcher *watcher
}

// New builds a Daemon from a Config. It validates/defaults the config, builds
// the Snooze client and the apiserver watcher, but performs no network I/O —
// Run drives the actual watch + forward loop.
func New(cfg Config, logger *slog.Logger) (*Daemon, error) {
	cfg, err := cfg.WithDefaults()
	if err != nil {
		return nil, err
	}
	if logger == nil {
		logger = slog.Default()
	}

	sc, err := snoozeclient.New(snoozeclient.Options{
		BaseURL:  cfg.Server,
		Username: cfg.Username,
		Password: cfg.Password,
		Method:   cfg.Method,
		Token:    cfg.Token,
		Insecure: cfg.Insecure,
		Timeout:  cfg.RequestTimeout,
		Logger:   logger,
	})
	if err != nil {
		return nil, fmt.Errorf("k8sevents: build snooze client: %w", err)
	}

	d := &Daemon{
		cfg:    cfg,
		logger: logger,
		snooze: sc,
		poster: sc,
	}
	w, err := newWatcher(cfg, logger, d.forward)
	if err != nil {
		return nil, err
	}
	d.watcher = w
	return d, nil
}

// newDaemonForTest wires a pre-built poster (e.g. one pointed at an httptest
// server, or a fake) so unit tests skip the snoozeclient construction dance.
func newDaemonForTest(cfg Config, poster snoozePoster, logger *slog.Logger) (*Daemon, error) {
	cfg, err := cfg.WithDefaults()
	if err != nil {
		return nil, err
	}
	if logger == nil {
		logger = slog.Default()
	}
	d := &Daemon{cfg: cfg, logger: logger, poster: poster}
	w, err := newWatcher(cfg, logger, d.forward)
	if err != nil {
		return nil, err
	}
	d.watcher = w
	return d, nil
}

// forward maps an Event to a Record and POSTs it to Snooze. It is the watcher's
// emit callback.
func (d *Daemon) forward(ctx context.Context, e Event) error {
	rec := d.cfg.ToRecord(e)
	if _, err := d.poster.PostAlert(ctx, rec); err != nil {
		return fmt.Errorf("k8sevents: post alert: %w", err)
	}
	d.logger.Debug("k8sevents: forwarded event",
		slog.String("severity", rec.Severity),
		slog.String("host", rec.Host),
		slog.String("process", rec.Process))
	return nil
}

// Run logs in to Snooze (best effort) then drives the watch loop until ctx is
// cancelled.
func (d *Daemon) Run(ctx context.Context) error {
	if d.snooze != nil && d.cfg.Token == "" && d.cfg.Username != "" {
		if err := d.snooze.Login(ctx); err != nil {
			d.logger.Warn("k8sevents: initial snooze login failed; will retry lazily",
				slog.Any("err", err))
		}
	}
	d.logger.Info("k8sevents: starting watch",
		slog.String("apiserver", d.cfg.APIServer),
		slog.String("namespace", nsLabel(d.cfg.Namespace)),
		slog.Bool("include_normal", d.cfg.IncludeNormal))
	return d.watcher.Run(ctx)
}

func nsLabel(ns string) string {
	if ns == "" {
		return "<all>"
	}
	return ns
}
