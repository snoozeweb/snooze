package snmptrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/japannext/snooze/pkg/snoozeclient"
)

// defaultQueueSize bounds the in-process backlog of traps awaiting forwarding.
// Sized to absorb a short burst without blocking the listener while still
// applying back-pressure on a truly stuck upstream.
const defaultQueueSize = 1024

// Daemon owns the listener and the forwarding worker. It is the unit that
// `cmd/snooze-snmptrap` orchestrates.
type Daemon struct {
	cfg       Config
	logger    *slog.Logger
	listener  *Listener
	forwarder *Forwarder

	queueSize int
}

// NewDaemon constructs a Daemon from the parsed config. The embedded
// snoozeclient.Client is created here so all auth/transport knobs land in one
// place. logger may be nil.
func NewDaemon(cfg Config, logger *slog.Logger) (*Daemon, error) {
	if logger == nil {
		logger = slog.Default()
	}
	client, err := snoozeclient.New(snoozeclient.Options{
		BaseURL:  cfg.Server,
		Username: cfg.Username,
		Password: cfg.Password,
		Method:   cfg.Method,
		Insecure: cfg.Insecure,
		Timeout:  cfg.Timeout,
		Logger:   logger,
	})
	if err != nil {
		return nil, fmt.Errorf("snmptrap: build client: %w", err)
	}
	d := &Daemon{
		cfg:       cfg,
		logger:    logger,
		forwarder: NewForwarder(client, logger),
		queueSize: defaultQueueSize,
	}
	return d, nil
}

// newDaemonWithDeps is the test-friendly constructor that lets the caller
// inject an alertPoster (and skip the real HTTP client). Kept unexported so
// it doesn't widen the public API.
func newDaemonWithDeps(cfg Config, logger *slog.Logger, poster alertPoster) *Daemon {
	if logger == nil {
		logger = slog.Default()
	}
	return &Daemon{
		cfg:       cfg,
		logger:    logger,
		forwarder: &Forwarder{client: poster, logger: logger, now: time.Now},
		queueSize: defaultQueueSize,
	}
}

// Run starts the listener and the forwarder worker. It blocks until ctx is
// cancelled or the listener errors out, returning the first non-nil error.
func (d *Daemon) Run(ctx context.Context) error {
	// Bounded channel: the listener publishes, the worker consumes. We size
	// it from queueSize so tests can shrink it when needed.
	queue := make(chan ParsedTrap, d.queueSize)

	d.listener = NewListener(d.cfg, d.logger, func(p ParsedTrap) {
		select {
		case queue <- p:
		default:
			// Back-pressure: drop on the floor and log. The alternative is
			// blocking gosnmp's read loop, which would queue traps in the
			// kernel UDP buffer instead — same end result, less visibility.
			d.logger.Warn("snmptrap: queue full, dropping trap",
				slog.String("host", p.Host),
				slog.String("process", p.Process),
			)
		}
	})

	// workerCtx scopes the forwarder goroutine. Closing the queue triggers
	// its return; cancelling workerCtx aborts any in-flight POST.
	workerCtx, cancelWorker := context.WithCancel(ctx)
	defer cancelWorker()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		d.forwardLoop(workerCtx, queue)
	}()

	listenErr := d.listener.Start(ctx)

	// Drain & shutdown sequence: stop accepting new traps, close the queue,
	// let the forwarder finish what it has buffered, then return.
	close(queue)
	wg.Wait()

	if listenErr != nil && !errors.Is(listenErr, context.Canceled) {
		return listenErr
	}
	return nil
}

// forwardLoop drains the trap queue and POSTs each entry. Context cancellation
// stops the loop after the in-flight call, queue closure stops it cleanly.
func (d *Daemon) forwardLoop(ctx context.Context, queue <-chan ParsedTrap) {
	for {
		select {
		case <-ctx.Done():
			return
		case p, ok := <-queue:
			if !ok {
				return
			}
			if err := d.forwarder.Forward(ctx, p); err != nil {
				// Forwarder already logged the error; we keep the worker
				// alive so a single failing alert doesn't kill the daemon.
				continue
			}
		}
	}
}
