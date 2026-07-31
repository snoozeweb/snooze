package syncer

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// defaultDebounce is the burst-coalescing window applied between an event and
// the subsequent Reload call.
const defaultDebounce = 100 * time.Millisecond

// defaultReloadTimeout bounds a single plug.Reload call. A reload that blocks
// past this (e.g. a wedged DB cursor) is abandoned so the single per-plugin
// dispatch goroutine cannot be stalled forever — the failure mode that freezes
// a plugin's in-memory cache at its last-loaded state while the server keeps
// serving. Generous relative to a healthy reload (which is a single indexed
// query), tight enough to recover from a stall within one window.
const defaultReloadTimeout = 30 * time.Second

// maxDebounceFactor caps how long a burst can postpone a reload. The debounce
// timer restarts on every event, so a stream of events arriving closer together
// than the debounce window would otherwise defer the reload indefinitely — a
// busy collection could starve its own cache refresh. Once a window has been
// open for maxDebounceFactor × debounce, the next event fires the reload
// instead of extending the wait.
const maxDebounceFactor = 10

// defaultSafetyReload is the cadence of the belt-and-braces full reload, used
// when Syncer.SafetyReload is zero. Event delivery is the fast path; this is
// the backstop that bounds how long a lost, misrouted, or dropped event can
// leave a cache stale. Set SafetyReload to a negative duration to disable it.
const defaultSafetyReload = 5 * time.Minute

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

// TenantLister enumerates the active tenant IDs. The Syncer needs it because a
// per-tenant plugin's Reload only refreshes the tenant carried in its context,
// while some events legitimately arrive without one:
//
//   - a delete carries no full document, so the source cannot name its tenant;
//   - a plugin-level (non-collection) event has no document at all.
//
// Without a lister those events reload under a naked context, which every
// tenant-scoped plugin correctly treats as "nothing to do" — silently. Wiring
// this makes them fan out to every active tenant instead.
type TenantLister func(ctx context.Context) ([]string, error)

// Syncer wires a Bus to a set of Pluggable consumers: any event matching a
// plugin's collection topic triggers a debounced Reload on that plugin.
type Syncer struct {
	Bus      Bus
	Plugins  map[string]Pluggable
	Debounce time.Duration
	// ReloadTimeout bounds each plug.Reload call. Zero selects
	// defaultReloadTimeout. See reloadBounded for why this matters.
	ReloadTimeout time.Duration
	// Tenants enumerates active tenants so tenant-less events can be fanned
	// out. Optional; nil keeps the old behaviour (a naked reload only).
	Tenants TenantLister
	// SafetyReload is the cadence of the periodic full reload. Zero selects
	// defaultSafetyReload; a negative value disables it.
	SafetyReload time.Duration
	Logger       *slog.Logger
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

// dispatchLoop coalesces a burst of events into Reload calls using a debounce
// window. The timer is started on the first event after an idle period and
// reset whenever a fresh event arrives within the window. When the window
// closes, the loop reloads once per distinct tenant seen during the window —
// NOT once total for whichever tenant arrived last. This matters under
// concurrent multi-tenant writes: an edit in tenant A and an edit in tenant B
// landing inside the same window must both refresh their respective per-tenant
// caches. Each tenant's Event.Tenant is threaded into the Reload context via
// snoozetypes.WithTenant so tenant-scoped driver queries inside Reload are
// correctly injected. The empty tenant (a delete, a global collection, or a
// plugin-level event) is a valid key on its own: it reloads with a naked ctx
// and, when a TenantLister is wired, also fans out to every active tenant —
// see reloadTenants.
//
// Two guards keep the loop from going quiet: the debounce window cannot be
// extended past maxDebounceFactor windows, and a periodic safety tick reloads
// every tenant regardless of events.
func (s *Syncer) dispatchLoop(ctx context.Context, name string, plug Pluggable, in <-chan Event, debounce time.Duration, logger *slog.Logger) {
	var timer *time.Timer
	var timerC <-chan time.Time
	// windowOpened marks when the current debounce window started, so a
	// continuous event stream cannot postpone the reload past maxDebounceFactor
	// windows. Meaningful only while timer != nil.
	var windowOpened time.Time
	maxWait := time.Duration(maxDebounceFactor) * debounce
	// pending is the set of distinct tenants whose reload is owed this window.
	// The empty string is a legitimate member (global / plugin-level events).
	pending := make(map[string]struct{})

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

	safetyTicker, safetyC := s.newSafetyTicker()
	if safetyTicker != nil {
		defer safetyTicker.Stop()
	}

	for {
		select {
		case <-ctx.Done():
			stopTimer()
			return
		case ev, ok := <-in:
			if !ok {
				stopTimer()
				return
			}
			pending[ev.Tenant] = struct{}{}
			if timer == nil {
				windowOpened = time.Now()
				timer = time.NewTimer(debounce)
				timerC = timer.C
			} else {
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				// Extend by one debounce window, but never past maxWait from
				// the moment the window opened.
				wait := debounce
				if remaining := maxWait - time.Since(windowOpened); remaining < wait {
					wait = max(remaining, 0)
				}
				timer.Reset(wait)
			}
		case <-timerC:
			timer, timerC = nil, nil
			if len(pending) == 0 {
				continue
			}
			tenants := pending
			pending = make(map[string]struct{})
			s.reloadTenants(ctx, name, plug, tenants, logger)
		case <-safetyC:
			// Belt and braces: refresh every tenant on a slow cadence so a lost
			// or misrouted event cannot leave a cache stale until restart.
			s.reloadTenants(ctx, name, plug, map[string]struct{}{"": {}}, logger)
		}
	}
}

// newSafetyTicker returns the periodic full-reload ticker, or (nil, nil) when
// the backstop is disabled.
func (s *Syncer) newSafetyTicker() (*time.Ticker, <-chan time.Time) {
	interval := s.SafetyReload
	if interval == 0 {
		interval = defaultSafetyReload
	}
	if interval < 0 {
		return nil, nil
	}
	t := time.NewTicker(interval)
	return t, t.C
}

// reloadTenants runs one Reload per distinct tenant in the set. A tenant-less
// entry means "the source could not name a tenant" (a delete, a plugin-level
// event, or the safety tick), so it expands to every active tenant — plus the
// naked reload itself, which is what a genuinely global collection needs.
//
// Expanding matters because a tenant-scoped plugin's Reload treats a naked
// context as a no-op: before this, a delete or an untenanted event simply never
// reached any cache, and did so without a trace in the logs.
func (s *Syncer) reloadTenants(ctx context.Context, name string, plug Pluggable, tenants map[string]struct{}, logger *slog.Logger) {
	if _, global := tenants[""]; global && s.Tenants != nil {
		ids, err := s.Tenants(ctx)
		if err != nil {
			logger.Warn("syncer: list tenants for reload fan-out failed; falling back to the naked reload",
				"plugin", name, "err", err)
		}
		for _, id := range ids {
			if id != "" {
				tenants[id] = struct{}{}
			}
		}
	}
	for tenant := range tenants {
		reloadCtx := ctx
		if tenant != "" {
			reloadCtx = snoozetypes.WithTenant(ctx, tenant)
		}
		s.reloadBounded(reloadCtx, name, plug, tenant, logger)
	}
}

// reloadBounded runs plug.Reload(ctx) under a timeout. The reload executes in
// its own goroutine and the dispatch loop waits on either completion or the
// timeout — so a Reload that wedges (a hung DB query that ignores cancellation,
// say) can stall the loop for at most ReloadTimeout instead of forever. Without
// this bound a single stuck reload silently freezes the plugin's cache: the
// dispatch goroutine never returns to drain its event channel, every later
// change event is dropped, and no reload ever runs again until the process is
// restarted.
//
// A timed-out reload's goroutine may linger (it owns a cancelled ctx and will
// unwind when its blocked operation honours cancellation). Reloads are
// idempotent full-state rebuilds guarded by the plugin's own lock, so a lagging
// reload overlapping a fresh one is safe.
func (s *Syncer) reloadBounded(ctx context.Context, name string, plug Pluggable, tenant string, logger *slog.Logger) {
	timeout := s.ReloadTimeout
	if timeout <= 0 {
		timeout = defaultReloadTimeout
	}
	rctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- plug.Reload(rctx) }()

	select {
	case err := <-done:
		if err != nil {
			logger.Warn("syncer: reload failed", "plugin", name, "tenant", tenant, "err", err)
		}
	case <-rctx.Done():
		logger.Warn("syncer: reload timed out; abandoned to keep the syncer live",
			"plugin", name, "tenant", tenant, "timeout", timeout)
	}
}
