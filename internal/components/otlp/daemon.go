package otlp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/snoozeweb/snooze/pkg/snoozeclient"
)

// Daemon ties the OTLP/HTTP receiver to a snoozeclient.Client. It owns no extra
// state — cmd/snooze-otlp/main.go stays trivial.
type Daemon struct {
	cfg    Config
	client *snoozeclient.Client
	server *server
	logger *slog.Logger
}

// New builds a Daemon from a Config. It validates the config and constructs the
// Snooze client up-front, but performs no network I/O — Login is deferred to
// Run so a misconfiguration surfaces before the listener is bound.
func New(cfg Config, logger *slog.Logger) (*Daemon, error) {
	cfg, err := cfg.WithDefaults()
	if err != nil {
		return nil, err
	}
	if logger == nil {
		logger = slog.Default()
	}
	client, err := snoozeclient.New(snoozeclient.Options{
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
		return nil, fmt.Errorf("otlp: build snooze client: %w", err)
	}
	return &Daemon{
		cfg:    cfg,
		client: client,
		server: newServer(cfg.Listen, client, logger),
		logger: logger,
	}, nil
}

// newDaemonWithPoster is the test entry point: it wires a pre-built record
// poster (e.g. one pointed at httptest.NewServer) without resolving a real
// Snooze client.
func newDaemonWithPoster(cfg Config, poster recordPoster, logger *slog.Logger) (*Daemon, error) {
	cfg, err := cfg.WithDefaults()
	if err != nil {
		return nil, err
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Daemon{
		cfg:    cfg,
		server: newServer(cfg.Listen, poster, logger),
		logger: logger,
	}, nil
}

// Addr returns the bound receiver address. Blocks until Run has started.
func (d *Daemon) Addr() string {
	if d.server == nil {
		return ""
	}
	return d.server.Addr()
}

// Run drives the receiver until ctx is cancelled. It performs a best-effort
// initial Snooze login when credentials are present so the first export doesn't
// pay the auth cost; the snoozeclient re-logs in lazily on a 401 regardless.
func (d *Daemon) Run(ctx context.Context) error {
	if d.client != nil && d.cfg.Token == "" && d.cfg.Username != "" {
		if err := d.client.Login(ctx); err != nil {
			d.logger.Warn("otlp: initial snooze login failed; will retry lazily",
				slog.Any("err", err))
		}
	}
	d.logger.Info("otlp: starting receiver",
		slog.String("listen", d.cfg.Listen),
		slog.String("server", d.cfg.Server),
	)
	if err := d.server.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}
