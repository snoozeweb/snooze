package syncer

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/snoozeweb/snooze/pkg/snoozetypes"
	"github.com/stretchr/testify/require"
)

// fakeBus is a minimal in-memory Bus for unit-testing the Syncer. It mirrors
// the semantics of the production inproc bus but stays in-package.
type fakeBus struct {
	mu     sync.Mutex
	subs   []*fakeSub
	closed bool
}

type fakeSub struct {
	prefix string
	ch     chan Event
	ctx    context.Context
	once   sync.Once
}

func newFakeBus() *fakeBus { return &fakeBus{} }

func (b *fakeBus) Publish(_ context.Context, e Event) error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	subs := append([]*fakeSub(nil), b.subs...)
	b.mu.Unlock()
	for _, s := range subs {
		if s.prefix != "" && !strings.HasPrefix(e.Topic, s.prefix) {
			continue
		}
		select {
		case s.ch <- e:
		default:
		}
	}
	return nil
}

func (b *fakeBus) Subscribe(ctx context.Context, topicPrefix string) (<-chan Event, error) {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		ch := make(chan Event)
		close(ch)
		return ch, nil
	}
	s := &fakeSub{
		prefix: topicPrefix,
		ch:     make(chan Event, 32),
		ctx:    ctx,
	}
	b.subs = append(b.subs, s)
	b.mu.Unlock()
	go func() {
		<-ctx.Done()
		b.mu.Lock()
		for i, cur := range b.subs {
			if cur == s {
				b.subs = append(b.subs[:i], b.subs[i+1:]...)
				break
			}
		}
		b.mu.Unlock()
		s.once.Do(func() { close(s.ch) })
	}()
	return s.ch, nil
}

func (b *fakeBus) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil
	}
	b.closed = true
	for _, s := range b.subs {
		s.once.Do(func() { close(s.ch) })
	}
	b.subs = nil
	return nil
}

// recordingPlugin counts Reload invocations for assertions.
type recordingPlugin struct {
	name      string
	count     int64
	reloadErr error
	hook      func()
	deps      []string // extra collections this plugin's state derives from
}

func (p *recordingPlugin) Name() string { return p.name }

// ReloadCollections satisfies the syncer's ReloadDeps interface. Returns nil
// (no extra subscriptions) unless the test populated deps.
func (p *recordingPlugin) ReloadCollections() []string { return p.deps }

func (p *recordingPlugin) Reload(_ context.Context) error {
	atomic.AddInt64(&p.count, 1)
	if p.hook != nil {
		p.hook()
	}
	return p.reloadErr
}

func (p *recordingPlugin) Count() int64 { return atomic.LoadInt64(&p.count) }

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

// TestSyncer_ReloadOnCollectionEvent verifies that a `collection.<name>` event
// triggers Reload after the debounce window.
func TestSyncer_ReloadOnCollectionEvent(t *testing.T) {
	bus := newFakeBus()
	defer bus.Close()
	plug := &recordingPlugin{name: "rule"}
	s := &Syncer{
		Bus:      bus,
		Plugins:  map[string]Pluggable{plug.Name(): plug},
		Debounce: 20 * time.Millisecond,
		Logger:   quietLogger(),
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()

	// Give Run a moment to subscribe before publishing.
	require.Eventually(t, func() bool {
		bus.mu.Lock()
		defer bus.mu.Unlock()
		return len(bus.subs) >= 2
	}, time.Second, 5*time.Millisecond, "subscriptions not registered")

	require.NoError(t, bus.Publish(ctx, Event{Topic: "collection.rule", Op: "write", Collection: "rule"}))

	require.Eventually(t, func() bool { return plug.Count() == 1 },
		time.Second, 5*time.Millisecond, "reload not invoked")

	cancel()
	require.NoError(t, <-done)
}

// TestSyncer_ReloadOnDependencyCollectionEvent verifies that a plugin which
// declares extra collection dependencies (via ReloadCollections) is reloaded
// when one of those collections changes — not just its own. The notification
// plugin relies on this: it caches the `action` collection, so an action edit
// must refresh it even though it owns the `notification` collection.
func TestSyncer_ReloadOnDependencyCollectionEvent(t *testing.T) {
	bus := newFakeBus()
	defer bus.Close()
	plug := &recordingPlugin{name: "notification", deps: []string{"action"}}
	s := &Syncer{
		Bus:      bus,
		Plugins:  map[string]Pluggable{plug.Name(): plug},
		Debounce: 20 * time.Millisecond,
		Logger:   quietLogger(),
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()

	// Three subscriptions expected: plugin.notification, collection.notification,
	// and the declared dependency collection.action.
	require.Eventually(t, func() bool {
		bus.mu.Lock()
		defer bus.mu.Unlock()
		return len(bus.subs) >= 3
	}, time.Second, 5*time.Millisecond, "dependency collection subscription not registered")

	require.NoError(t, bus.Publish(ctx, Event{Topic: "collection.action", Op: "write", Collection: "action"}))
	require.Eventually(t, func() bool { return plug.Count() == 1 },
		time.Second, 5*time.Millisecond, "reload not invoked on dependency collection event")

	cancel()
	require.NoError(t, <-done)
}

// TestSyncer_DebounceCoalesces verifies that a burst of events within the
// debounce window collapses to a single Reload.
func TestSyncer_DebounceCoalesces(t *testing.T) {
	bus := newFakeBus()
	defer bus.Close()
	plug := &recordingPlugin{name: "rule"}
	s := &Syncer{
		Bus:      bus,
		Plugins:  map[string]Pluggable{plug.Name(): plug},
		Debounce: 50 * time.Millisecond,
		Logger:   quietLogger(),
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()

	require.Eventually(t, func() bool {
		bus.mu.Lock()
		defer bus.mu.Unlock()
		return len(bus.subs) >= 2
	}, time.Second, 5*time.Millisecond, "subscriptions not registered")

	// Publish a burst well inside the debounce window.
	for i := 0; i < 10; i++ {
		require.NoError(t, bus.Publish(ctx, Event{Topic: "collection.rule", Op: "write", Collection: "rule"}))
		time.Sleep(2 * time.Millisecond)
	}

	require.Eventually(t, func() bool { return plug.Count() >= 1 },
		2*time.Second, 5*time.Millisecond, "reload not invoked")
	// Give the timer at least one extra debounce window to confirm no extra
	// reloads fire from the same burst.
	time.Sleep(100 * time.Millisecond)
	require.Equal(t, int64(1), plug.Count(), "expected debounce to coalesce events")

	cancel()
	require.NoError(t, <-done)
}

// TestSyncer_PluginTopic verifies that a `plugin.<name>` event also triggers
// reload (independently of the collection topic).
func TestSyncer_PluginTopic(t *testing.T) {
	bus := newFakeBus()
	defer bus.Close()
	plug := &recordingPlugin{name: "rule"}
	s := &Syncer{
		Bus:      bus,
		Plugins:  map[string]Pluggable{plug.Name(): plug},
		Debounce: 20 * time.Millisecond,
		Logger:   quietLogger(),
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()

	require.Eventually(t, func() bool {
		bus.mu.Lock()
		defer bus.mu.Unlock()
		return len(bus.subs) >= 2
	}, time.Second, 5*time.Millisecond)

	require.NoError(t, bus.Publish(ctx, Event{Topic: "plugin.rule", Op: "reload"}))
	require.Eventually(t, func() bool { return plug.Count() == 1 },
		time.Second, 5*time.Millisecond)

	cancel()
	require.NoError(t, <-done)
}

// TestSyncer_ReloadErrorLoggedAndContinues verifies that a failing Reload does
// not stop the syncer: subsequent events still trigger reloads.
func TestSyncer_ReloadErrorLoggedAndContinues(t *testing.T) {
	bus := newFakeBus()
	defer bus.Close()
	plug := &recordingPlugin{name: "rule", reloadErr: errors.New("boom")}
	s := &Syncer{
		Bus:      bus,
		Plugins:  map[string]Pluggable{plug.Name(): plug},
		Debounce: 10 * time.Millisecond,
		Logger:   quietLogger(),
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()

	require.Eventually(t, func() bool {
		bus.mu.Lock()
		defer bus.mu.Unlock()
		return len(bus.subs) >= 2
	}, time.Second, 5*time.Millisecond)

	require.NoError(t, bus.Publish(ctx, Event{Topic: "collection.rule", Op: "write"}))
	require.Eventually(t, func() bool { return plug.Count() >= 1 },
		time.Second, 5*time.Millisecond)

	// Wait past the debounce window so the next event is treated fresh.
	time.Sleep(30 * time.Millisecond)
	require.NoError(t, bus.Publish(ctx, Event{Topic: "collection.rule", Op: "write"}))
	require.Eventually(t, func() bool { return plug.Count() >= 2 },
		time.Second, 5*time.Millisecond)

	cancel()
	require.NoError(t, <-done)
}

// TestSyncer_NoLeakedGoroutines verifies Run cleans up after ctx cancellation.
func TestSyncer_NoLeakedGoroutines(t *testing.T) {
	// Settle background goroutines.
	time.Sleep(50 * time.Millisecond)
	runtime.GC()
	before := runtime.NumGoroutine()

	bus := newFakeBus()
	plug := &recordingPlugin{name: "rule"}
	s := &Syncer{
		Bus:      bus,
		Plugins:  map[string]Pluggable{plug.Name(): plug},
		Debounce: 10 * time.Millisecond,
		Logger:   quietLogger(),
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()

	require.Eventually(t, func() bool {
		bus.mu.Lock()
		defer bus.mu.Unlock()
		return len(bus.subs) >= 2
	}, time.Second, 5*time.Millisecond)

	// Cause some work.
	for i := 0; i < 5; i++ {
		_ = bus.Publish(ctx, Event{Topic: "collection.rule"})
	}
	require.Eventually(t, func() bool { return plug.Count() >= 1 },
		time.Second, 5*time.Millisecond)

	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
	_ = bus.Close()

	// Allow goroutines a moment to unwind.
	require.Eventually(t, func() bool {
		runtime.GC()
		return runtime.NumGoroutine() <= before+1
	}, 2*time.Second, 20*time.Millisecond, "syncer leaked goroutines: before=%d, after=%d", before, runtime.NumGoroutine())
}

// TestSyncer_EmptyPluginsRespectsContext verifies an empty plugin map still
// returns cleanly on cancel rather than blocking forever.
func TestSyncer_EmptyPluginsRespectsContext(t *testing.T) {
	bus := newFakeBus()
	defer bus.Close()
	s := &Syncer{Bus: bus, Logger: quietLogger()}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("Run did not exit on cancel with empty plugins")
	}
}

// TestSyncer_NilBus returns an error rather than panicking.
func TestSyncer_NilBus(t *testing.T) {
	s := &Syncer{}
	err := s.Run(context.Background())
	require.Error(t, err)
}

// tenantCapturingPlugin is like recordingPlugin but also extracts and reports
// the tenant slug from the Reload context.
type tenantCapturingPlugin struct {
	name     string
	tenantCh chan string
}

func (p *tenantCapturingPlugin) Name() string { return p.name }
func (p *tenantCapturingPlugin) Reload(ctx context.Context) error {
	t, _ := snoozetypes.TenantFrom(ctx)
	select {
	case p.tenantCh <- t:
	default:
	}
	return nil
}

// TestSyncer_TenantContextOnReload verifies that when an event carries a
// Tenant slug the Syncer's Reload is called with that tenant in context.
func TestSyncer_TenantContextOnReload(t *testing.T) {
	bus := newFakeBus()
	defer bus.Close()

	capturedTenant := make(chan string, 1)
	plug := &tenantCapturingPlugin{name: "rule", tenantCh: capturedTenant}
	s := &Syncer{
		Bus:      bus,
		Plugins:  map[string]Pluggable{plug.Name(): plug},
		Debounce: 20 * time.Millisecond,
		Logger:   quietLogger(),
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()

	require.Eventually(t, func() bool {
		bus.mu.Lock()
		defer bus.mu.Unlock()
		return len(bus.subs) >= 2
	}, time.Second, 5*time.Millisecond, "subscriptions not registered")

	require.NoError(t, bus.Publish(ctx, Event{
		Topic:      CollectionTopic("rule", "acme"),
		Op:         "write",
		Collection: "rule",
		Tenant:     "acme",
	}))

	select {
	case got := <-capturedTenant:
		require.Equal(t, "acme", got)
	case <-time.After(time.Second):
		t.Fatal("reload not invoked with tenant context")
	}

	cancel()
	require.NoError(t, <-done)
}
