package syncer

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
)

// defaultDebounce is the burst-coalescing window applied between an event and
// the subsequent Reload call.
const defaultDebounce = 100 * time.Millisecond

// Pluggable is the slice of the plugin contract the Syncer needs: a name and a
// Reload method that refreshes in-memory state from the database.
type Pluggable interface {
	Name() string
	Reload(ctx context.Context) error
}

// ReloadDeps is an optional interface a Pluggable implements when its in-memory
// state derives from collections other than its own. The Syncer subscribes such
// a plugin to those collections' change topics too, so an edit to a dependency
// collection triggers the plugin's Reload. Returning nil/empty means "no extra
// dependencies" (the default for plugins that only watch their own collection).
type ReloadDeps interface {
	ReloadCollections() []string
}

// Syncer wires a Bus to a set of Pluggable consumers: any event matching a
// plugin's collection topic triggers a debounced Reload on that plugin.
type Syncer struct {
	Bus      Bus
	Plugins  map[string]Pluggable
	Debounce time.Duration
	Logger   *slog.Logger
}

// Run subscribes to one fan-in stream per plugin and dispatches debounced
// Reload calls until ctx is cancelled. Subscription / reload errors are
// logged; Run only returns a non-nil error if the Bus itself rejects a
// subscription at start-up.
func (s *Syncer) Run(ctx context.Context) error {
	if s.Bus == nil {
		return fmt.Errorf("syncer: nil Bus")
	}
	if len(s.Plugins) == 0 {
		// Nothing to do; honour ctx and return when cancelled.
		<-ctx.Done()
		return nil
	}
	logger := s.Logger
	if logger == nil {
		logger = slog.Default()
	}
	debounce := s.Debounce
	if debounce <= 0 {
		debounce = defaultDebounce
	}

	g, gctx := errgroup.WithContext(ctx)
	for name, plug := range s.Plugins {
		g.Go(func() error {
			s.runPlugin(gctx, name, plug, debounce, logger)
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return fmt.Errorf("syncer: run: %w", err)
	}
	return nil
}

// runPlugin owns one plugin's subscriptions and debouncing state. It is the
// only goroutine that ever invokes plug.Reload, so callers don't have to
// guard against concurrent reloads.
func (s *Syncer) runPlugin(ctx context.Context, name string, plug Pluggable, debounce time.Duration, logger *slog.Logger) {
	topics := []string{
		"plugin." + name,
		"collection." + name,
	}
	// A plugin whose in-memory state derives from other collections (e.g. the
	// notification plugin caches the `action` collection) declares them via
	// ReloadDeps so an edit to a dependency collection also triggers its Reload.
	// Without this, edits to those collections only take effect on restart, on
	// the plugin's own collection changing, or on a cache miss for a *new* key —
	// never on an edit to an existing one.
	if dep, ok := plug.(ReloadDeps); ok {
		for _, c := range dep.ReloadCollections() {
			if c == "" || c == name {
				continue // own collection already covered
			}
			topics = append(topics, "collection."+c)
		}
	}
	merged := make(chan Event, 64)

	var wg sync.WaitGroup
	for _, t := range topics {
		ch, err := s.Bus.Subscribe(ctx, t)
		if err != nil {
			logger.Warn("syncer: subscribe failed", "plugin", name, "topic", t, "err", err)
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case ev, ok := <-ch:
					if !ok {
						return
					}
					select {
					case merged <- ev:
					case <-ctx.Done():
						return
					}
				}
			}
		}()
	}

	// Closer goroutine: when every subscriber finishes (ctx cancellation or
	// bus close), close the merged channel so the dispatch loop exits cleanly.
	closeDone := make(chan struct{})
	go func() {
		wg.Wait()
		close(merged)
		close(closeDone)
	}()

	s.dispatchLoop(ctx, name, plug, merged, debounce, logger)

	// Block on the closer to avoid leaking goroutines on return.
	<-closeDone
}

// dispatchLoop coalesces a burst of events into a single Reload using a debounce
// window. The timer is started on the first event after an idle period and
// reset whenever a fresh event arrives within the window.
func (s *Syncer) dispatchLoop(ctx context.Context, name string, plug Pluggable, in <-chan Event, debounce time.Duration, logger *slog.Logger) {
	var timer *time.Timer
	var timerC <-chan time.Time
	pending := false

	stopTimer := func() {
		if timer != nil {
			if !timer.Stop() {
				// Drain if it already fired.
				select {
				case <-timer.C:
				default:
				}
			}
		}
	}

	for {
		select {
		case <-ctx.Done():
			stopTimer()
			return
		case _, ok := <-in:
			if !ok {
				stopTimer()
				return
			}
			pending = true
			if timer == nil {
				timer = time.NewTimer(debounce)
				timerC = timer.C
			} else {
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(debounce)
			}
		case <-timerC:
			if !pending {
				continue
			}
			pending = false
			if err := plug.Reload(ctx); err != nil {
				logger.Warn("syncer: reload failed", "plugin", name, "err", err)
			}
		}
	}
}
