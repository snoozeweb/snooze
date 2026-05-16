package relp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/snoozeweb/snooze/pkg/snoozeclient"
)

// Daemon owns the RELP listener and its Forwarder. Construct it with New
// and drive it with Run; Run blocks until the context is cancelled.
type Daemon struct {
	cfg       Config
	client    *snoozeclient.Client
	forwarder *Forwarder
	listener  *Listener
	logger    *slog.Logger
}

// Options bundles the dependencies New needs. The Client is mandatory; tests
// can inject a stub HTTP transport via snoozeclient.Options.HTTPClient.
type Options struct {
	Config Config
	Client *snoozeclient.Client
	Logger *slog.Logger
}

// New builds a Daemon. It validates Config, constructs the Forwarder and
// pre-binds the Listener wiring. It does NOT bind the TCP socket — that
// happens in Run.
func New(opts Options) (*Daemon, error) {
	cfg, err := opts.Config.WithDefaults()
	if err != nil {
		return nil, err
	}
	if opts.Client == nil {
		return nil, errors.New("relp: snooze client is required")
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}

	fwd, err := NewForwarder(opts.Client, cfg.Parser)
	if err != nil {
		return nil, err
	}

	l, err := NewListener(ListenerOptions{
		Addr:        cfg.Listen,
		Handler:     fwd.Forward,
		Logger:      logger,
		MaxFrameLen: cfg.MaxFrameBytes,
		ReadTimeout: cfg.ReadTimeout,
	})
	if err != nil {
		return nil, err
	}

	return &Daemon{
		cfg:       cfg,
		client:    opts.Client,
		forwarder: fwd,
		listener:  l,
		logger:    logger,
	}, nil
}

// Run logs into the Snooze server (unless a token is already set) and serves
// the listener until ctx is cancelled. Returns ctx.Err() on clean shutdown.
func (d *Daemon) Run(ctx context.Context) error {
	if d.client.Token() == "" && d.cfg.Username != "" {
		d.logger.Info("relp: logging in to snooze",
			slog.String("server", d.client.BaseURL()),
			slog.String("user", d.cfg.Username))
		if err := d.client.Login(ctx); err != nil {
			return fmt.Errorf("relp: login: %w", err)
		}
	}

	d.logger.Info("relp: daemon starting",
		slog.String("listen", d.cfg.Listen),
		slog.String("parser", d.cfg.Parser))

	if err := d.listener.Serve(ctx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

// Addr returns the bound listener address (useful in tests once the
// listener has started). Empty before Run.
func (d *Daemon) Addr() string { return d.listener.Addr() }
