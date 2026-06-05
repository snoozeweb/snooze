package jira

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/snoozeweb/snooze/pkg/snoozeclient"
)

// Daemon ties together the JIRA REST client, the inbound webhook server,
// and the optional bidirectional poller. It is the unit cmd/snooze-jira
// orchestrates.
type Daemon struct {
	cfg    Config
	logger *slog.Logger

	jira      *Client
	snooze    *snoozeclient.Client
	snoozeAPI snoozeAPI
	forwarder *forwarder
	server    *httpServer
	poller    *poller
}

// New builds a Daemon from a validated Config. It does no network I/O —
// callers should call Run to actually drive the bridge.
func New(cfg Config, logger *slog.Logger) (*Daemon, error) {
	cfg, err := cfg.WithDefaults()
	if err != nil {
		return nil, err
	}
	if logger == nil {
		logger = slog.Default()
	}

	jc := NewClient(ClientOptions{
		BaseURL:   cfg.JiraURL,
		Email:     cfg.JiraEmail,
		Token:     cfg.JiraAPIToken,
		VerifySSL: cfg.sslVerify(),
		Timeout:   cfg.RequestTimeout,
		Logger:    logger,
	})

	d := &Daemon{
		cfg:    cfg,
		logger: logger,
		jira:   jc,
	}
	d.forwarder = newForwarder(cfg, jc, logger)
	d.server = newHTTPServer(cfg.listenAddr(), d.forwarder, logger)

	// The poller is only constructed when both wanted *and* able to run.
	// We build the Snooze client lazily — if the operator has Server set but
	// no poller wanted, we still skip the client to avoid a useless DNS
	// lookup and token cache write.
	if cfg.pollerWanted() {
		if cfg.Server == "" {
			return nil, fmt.Errorf("%w: server (required when polling)", ErrMissingConfig)
		}
		sc, err := snoozeclient.New(snoozeclient.Options{
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
			return nil, fmt.Errorf("jira: build snooze client: %w", err)
		}
		d.snooze = sc
		d.snoozeAPI = snoozeClientAdapter{c: sc}
		d.poller = newPoller(cfg, jc, d.snoozeAPI, logger)
	}
	return d, nil
}

// Addr returns the bound webhook listener address. Empty until Run starts.
func (d *Daemon) Addr() string {
	if d.server == nil {
		return ""
	}
	return d.server.Addr()
}

// Run drives the daemon until ctx is cancelled. It returns the first
// non-context error encountered by the webhook server (the poller does not
// surface errors — they're logged and the loop continues).
func (d *Daemon) Run(ctx context.Context) error {
	if d.poller != nil && d.snooze != nil {
		// Best-effort Snooze login — the snoozeclient lazily re-logs in on
		// 401 so a transient failure here is not fatal.
		if d.cfg.Token == "" && d.cfg.Username != "" {
			if err := d.snooze.Login(ctx); err != nil {
				d.logger.Warn("jira: snooze login failed; will retry lazily",
					slog.Any("err", err))
			}
		}
	}

	var wg sync.WaitGroup
	var serverErr error
	pollCtx, cancelPoll := context.WithCancel(ctx)
	defer cancelPoll()

	if d.poller != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d.poller.Run(pollCtx)
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := d.server.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			serverErr = err
		}
		// When the server exits, also stop the poller so Run can return.
		cancelPoll()
	}()

	wg.Wait()
	return serverErr
}
