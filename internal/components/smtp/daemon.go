package smtp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/japannext/snooze/pkg/snoozeclient"
)

// Daemon glues together a snoozeclient.Client and an SMTP Server. It owns no
// extra state — the goal is to keep cmd/snooze-smtp/main.go trivial.
type Daemon struct {
	cfg    Config
	client *snoozeclient.Client
	server *Server
	logger *slog.Logger
	fwd    *Forwarder
}

// NewDaemon builds a Daemon from a parsed Config. It validates the config,
// initialises the Snooze HTTP client and wires the SMTP server's
// MessageHandler to a Forwarder. The HTTP client is created up-front but no
// network call is made — Login is deferred to Run so configuration errors
// don't sit between Listen() and the operator.
func NewDaemon(cfg Config, logger *slog.Logger) (*Daemon, error) {
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
		return nil, fmt.Errorf("smtp: build snooze client: %w", err)
	}

	fwd := NewForwarder(client, cfg)
	srv, err := NewServer(cfg, fwd.Forward, logger)
	if err != nil {
		return nil, err
	}
	return &Daemon{
		cfg:    cfg,
		client: client,
		server: srv,
		logger: logger,
		fwd:    fwd,
	}, nil
}

// NewDaemonWithClient is the test entry point: it wires a pre-built client
// (e.g. one pointed at httptest.NewServer) without going through the
// resolve-defaults dance again.
func NewDaemonWithClient(cfg Config, client *snoozeclient.Client, logger *slog.Logger) (*Daemon, error) {
	if client == nil {
		return nil, errors.New("smtp: NewDaemonWithClient requires a non-nil client")
	}
	cfg, err := cfg.WithDefaults()
	if err != nil {
		return nil, err
	}
	if logger == nil {
		logger = slog.Default()
	}
	fwd := NewForwarder(client, cfg)
	srv, err := NewServer(cfg, fwd.Forward, logger)
	if err != nil {
		return nil, err
	}
	return &Daemon{
		cfg:    cfg,
		client: client,
		server: srv,
		logger: logger,
		fwd:    fwd,
	}, nil
}

// LocalAddr returns the bound listener address (post-Listen).
func (d *Daemon) LocalAddr() string {
	if a := d.server.LocalAddr(); a != nil {
		return a.String()
	}
	return ""
}

// Listen binds the SMTP listener without entering the accept loop. Useful for
// tests that need the resolved port before starting the server goroutine.
func (d *Daemon) Listen() error { return d.server.Listen() }

// Run blocks running the SMTP listener until ctx is cancelled. It performs
// an initial login (when credentials are present) so the first inbound mail
// doesn't pay the auth cost.
func (d *Daemon) Run(ctx context.Context) error {
	if d.cfg.Token == "" && d.cfg.Username != "" {
		if err := d.client.Login(ctx); err != nil {
			d.logger.Warn("smtp: initial login failed, continuing (will retry on first POST)", slog.Any("err", err))
		}
	}
	d.logger.Info("smtp: starting SMTP listener",
		slog.String("listen", d.cfg.Listen),
		slog.String("server", d.cfg.Server),
		slog.Bool("starttls", d.cfg.TLSCert != ""),
		slog.Bool("auth_required", d.cfg.AuthRequired),
	)
	return d.server.Run(ctx)
}

// Close stops the SMTP listener. Safe to call concurrently with Run.
func (d *Daemon) Close() error { return d.server.Close() }
