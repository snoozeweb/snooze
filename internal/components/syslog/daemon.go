package syslog

import (
	"context"
	"fmt"
	"log/slog"

	"golang.org/x/sync/errgroup"

	"github.com/snoozeweb/snooze/pkg/snoozeclient"
)

// Daemon is the top-level orchestrator for snooze-syslog. It owns the
// snoozeclient, the parser, and one or two listeners (UDP and/or TCP).
//
// Lifecycle: build with New, then call Run(ctx). Run blocks until ctx is
// cancelled or a listener fails fatally; it always returns the cause.
type Daemon struct {
	cfg       Config
	client    *snoozeclient.Client
	parser    *MessageParser
	forwarder *Forwarder
	udp       *UDPListener
	tcp       *TCPListener
	logger    *slog.Logger
}

// New builds a Daemon from cfg. Heavy resources (network sockets, HTTP client)
// are created here so Run is a tight, fast-failing loop. Use cfg.WithDefaults
// before calling New if you constructed the Config in code rather than via
// LoadConfig.
func New(cfg Config, logger *slog.Logger) (*Daemon, error) {
	if logger == nil {
		logger = slog.Default()
	}
	cfg, err := cfg.WithDefaults()
	if err != nil {
		return nil, err
	}

	client, err := snoozeclient.New(snoozeclient.Options{
		BaseURL:     cfg.Server,
		Username:    cfg.Username,
		Password:    cfg.Password,
		Method:      cfg.Method,
		Token:       cfg.Token,
		IngestToken: cfg.IngestToken,
		Insecure:    cfg.Insecure,
		Timeout:     cfg.RequestTimeout,
		Logger:      logger,
	})
	if err != nil {
		return nil, fmt.Errorf("syslog: build snooze client: %w", err)
	}

	parser, err := NewParser(cfg.Parser)
	if err != nil {
		return nil, err
	}
	forwarder := NewForwarder(client)

	d := &Daemon{
		cfg:       cfg,
		client:    client,
		parser:    parser,
		forwarder: forwarder,
		logger:    logger,
	}
	if cfg.ListenUDP != "" {
		udp, err := NewUDPListener(cfg.ListenUDP, parser, forwarder, logger)
		if err != nil {
			return nil, err
		}
		d.udp = udp
	}
	if cfg.ListenTCP != "" {
		tcp, err := NewTCPListener(cfg.ListenTCP, parser, forwarder, logger)
		if err != nil {
			if d.udp != nil {
				// Release the UDP socket if the TCP bind fails — otherwise the
				// caller would leak it on the error path.
				_ = d.udp.conn.Close()
			}
			return nil, err
		}
		d.tcp = tcp
	}
	return d, nil
}

// Client returns the underlying snoozeclient.Client, primarily so callers
// (or tests) can perform the initial Login outside of Run.
func (d *Daemon) Client() *snoozeclient.Client { return d.client }

// UDPAddr returns the bound UDP address (or nil when UDP is disabled).
// Tests rely on this when binding to ":0".
func (d *Daemon) UDPAddr() string {
	if d.udp == nil {
		return ""
	}
	return d.udp.LocalAddr().String()
}

// TCPAddr returns the bound TCP address (or "" when TCP is disabled).
func (d *Daemon) TCPAddr() string {
	if d.tcp == nil {
		return ""
	}
	return d.tcp.LocalAddr().String()
}

// Run starts the configured listeners and blocks until ctx is cancelled or a
// listener returns a fatal error. The token cache and HTTP client are reused
// across the lifetime of the daemon — no per-message login.
//
// Login is performed lazily by the snoozeclient on the first 401, so a missing
// or stale token is recovered transparently as long as Username/Password are
// configured.
func (d *Daemon) Run(ctx context.Context) error {
	if d.udp == nil && d.tcp == nil {
		return fmt.Errorf("syslog: no listeners configured")
	}

	// Eagerly login so we surface bad credentials before traffic starts.
	if d.cfg.Token == "" && d.cfg.Username != "" {
		if err := d.client.Login(ctx); err != nil {
			d.logger.Warn("syslog: initial login failed (will retry lazily)", slog.Any("err", err))
		}
	}

	group, gctx := errgroup.WithContext(ctx)
	if d.udp != nil {
		group.Go(func() error {
			d.logger.Info("syslog: udp listener started", slog.String("addr", d.UDPAddr()))
			return d.udp.Run(gctx)
		})
	}
	if d.tcp != nil {
		group.Go(func() error {
			d.logger.Info("syslog: tcp listener started", slog.String("addr", d.TCPAddr()))
			return d.tcp.Run(gctx)
		})
	}
	return group.Wait()
}
