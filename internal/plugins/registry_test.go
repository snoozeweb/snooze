package plugins

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// recordingIndexDB wraps memDB and captures every CreateIndex call so the
// search_fields-registration path can be asserted without a real driver.
type recordingIndexDB struct {
	*memDB
	mu        sync.Mutex
	indexCols map[string][]string
	calls     int
	indexErr  error
}

func newRecordingIndexDB() *recordingIndexDB {
	return &recordingIndexDB{
		memDB:     newMemDB(),
		indexCols: map[string][]string{},
	}
}

func (r *recordingIndexDB) CreateIndex(_ context.Context, collection string, fields []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	if r.indexErr != nil {
		return r.indexErr
	}
	r.indexCols[collection] = append([]string(nil), fields...)
	return nil
}

func (r *recordingIndexDB) indexed(collection string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.indexCols[collection]...)
}

func (r *recordingIndexDB) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

// blockingIndexDriver parks inside CreateIndex until release is closed, which
// is how the background-index-pass test proves Build does not wait for it.
type blockingIndexDriver struct {
	*memDB
	enterOnce sync.Once
	relOnce   sync.Once
	entered   chan struct{} // closed when CreateIndex is first entered
	release   chan struct{} // CreateIndex returns once this is closed

	mu    sync.Mutex
	calls []string // collections whose CreateIndex has returned
}

// unblock releases a parked CreateIndex. Idempotent, so it is safe both as the
// test's own release point and as a t.Cleanup safety net — a driver left
// parked would otherwise hang every later WaitBackground in the package.
func (b *blockingIndexDriver) unblock() {
	b.relOnce.Do(func() { close(b.release) })
}

func newBlockingIndexDriver() *blockingIndexDriver {
	return &blockingIndexDriver{
		memDB:   newMemDB(),
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (b *blockingIndexDriver) CreateIndex(ctx context.Context, collection string, _ []string) error {
	b.enterOnce.Do(func() { close(b.entered) })
	select {
	case <-b.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls = append(b.calls, collection)
	return nil
}

func (b *blockingIndexDriver) completed() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.calls...)
}

// fakePlugin is a minimal Plugin that records the meta it was built with.
type fakePlugin struct {
	meta     Metadata
	post     int
	reload   int
	procImpl func(ctx context.Context, rec snoozetypes.Record) (Result, error)
}

func (f *fakePlugin) Name() string                             { return f.meta.Name }
func (f *fakePlugin) Metadata() Metadata                       { return f.meta }
func (f *fakePlugin) PostInit(_ context.Context, _ Host) error { f.post++; return nil }
func (f *fakePlugin) Reload(_ context.Context) error           { f.reload++; return nil }

// fakeProcessor adds Process to fakePlugin to satisfy Processor.
type fakeProcessor struct {
	fakePlugin
}

func (f *fakeProcessor) Process(ctx context.Context, rec snoozetypes.Record) (Result, error) {
	if f.procImpl != nil {
		return f.procImpl(ctx, rec)
	}
	return Result{Action: ActionContinue, Record: rec}, nil
}

func newFake(_ string) Factory {
	return func(meta Metadata) (Plugin, error) {
		return &fakePlugin{meta: meta}, nil
	}
}

func newFakeProcessor(_ string) Factory {
	return func(meta Metadata) (Plugin, error) {
		return &fakeProcessor{fakePlugin: fakePlugin{meta: meta}}, nil
	}
}

func TestRegister_BasicAndSorted(t *testing.T) {
	resetForTest()
	Register("zeta", []byte("name: zeta\n"), newFake("zeta"))
	Register("alpha", []byte("name: alpha\n"), newFake("alpha"))
	Register("mike", []byte("name: mike\n"), newFake("mike"))

	require.Equal(t, []string{"alpha", "mike", "zeta"}, Registered())
}

func TestRegister_DuplicatePanics(t *testing.T) {
	resetForTest()
	Register("dup", nil, newFake("dup"))
	require.PanicsWithValue(t, `plugins.Register: duplicate plugin name "dup"`, func() {
		Register("dup", nil, newFake("dup"))
	})
}

func TestRegister_EmptyNamePanics(t *testing.T) {
	resetForTest()
	require.Panics(t, func() { Register("", nil, newFake("x")) })
}

func TestRegister_NilFactoryPanics(t *testing.T) {
	resetForTest()
	require.Panics(t, func() { Register("x", nil, nil) })
}

func TestBuild_PostInitAndProcessorOrder(t *testing.T) {
	resetForTest()
	Register("rule", []byte("name: rule\n"), newFakeProcessor("rule"))
	Register("snooze", []byte("name: snooze\n"), newFakeProcessor("snooze"))
	Register("notif", []byte("name: notif\n"), newFake("notif")) // non-Processor

	host := &nullHost{}
	all, procs, err := Build(context.Background(), host, []string{"rule", "snooze", "missing", "notif"})
	require.NoError(t, err)
	require.Len(t, all, 3)
	require.Contains(t, all, "rule")
	require.Contains(t, all, "snooze")
	require.Contains(t, all, "notif")

	// PostInit called once per plugin.
	for _, p := range all {
		switch v := p.(type) {
		case *fakeProcessor:
			require.Equal(t, 1, v.post, "post-init count for %s", v.Name())
		case *fakePlugin:
			require.Equal(t, 1, v.post)
		}
	}

	// Processors filtered by order; "missing" silently skipped, "notif"
	// included in `all` but not a Processor → not in procs.
	require.Len(t, procs, 2)
	require.Equal(t, "rule", procs[0].Name())
	require.Equal(t, "snooze", procs[1].Name())
}

func TestBuild_TwicePanics(t *testing.T) {
	resetForTest()
	Register("only", nil, newFake("only"))
	_, _, err := Build(context.Background(), &nullHost{}, nil)
	require.NoError(t, err)
	require.Panics(t, func() {
		_, _, _ = Build(context.Background(), &nullHost{}, nil)
	})
}

func TestBuild_FactoryError(t *testing.T) {
	resetForTest()
	boom := errors.New("boom")
	Register("bad", []byte("name: bad\n"), func(_ Metadata) (Plugin, error) {
		return nil, boom
	})
	_, _, err := Build(context.Background(), &nullHost{}, nil)
	require.ErrorIs(t, err, boom)
}

func TestBuild_NilPluginIsRejected(t *testing.T) {
	resetForTest()
	Register("nilp", nil, func(_ Metadata) (Plugin, error) { return nil, nil })
	_, _, err := Build(context.Background(), &nullHost{}, nil)
	require.Error(t, err)
}

func TestBuild_MetadataParseError(t *testing.T) {
	resetForTest()
	Register("bad", []byte("not: : valid: yaml"), newFake("bad"))
	_, _, err := Build(context.Background(), &nullHost{}, nil)
	require.Error(t, err)
}

func TestBuild_NameFallback(t *testing.T) {
	// Metadata yaml lacks `name:`; Build should fill it from the registry key.
	resetForTest()
	Register("auto", nil, func(meta Metadata) (Plugin, error) {
		require.Equal(t, "auto", meta.Name)
		return &fakePlugin{meta: meta}, nil
	})
	_, _, err := Build(context.Background(), &nullHost{}, nil)
	require.NoError(t, err)
}

func TestBuild_RegistersSearchFieldsOnDriver(t *testing.T) {
	// Build must thread metadata.search_fields through to driver.CreateIndex
	// so the SEARCH condition operator (bare-word SearchBar input) can
	// resolve against the listed fields. Without this the driver's per-
	// collection registry stays empty and SEARCH matches nothing.
	resetForTest()
	Register("record", []byte(`
name: record
search_fields:
  - host
  - message
  - source
`), newFake("record"))
	Register("audit", []byte(`
name: audit
`), newFake("audit"))

	host := &nullHost{driver: newRecordingIndexDB()}
	_, _, err := Build(context.Background(), host, nil)
	require.NoError(t, err)

	drv := host.driver.(*recordingIndexDB)
	// The pass is dispatched to a background goroutine, so poll for it.
	require.Eventually(t, func() bool {
		return len(drv.indexed("record")) == 3
	}, 10*time.Second, 2*time.Millisecond, "search fields never reached the driver")
	require.Equal(t, []string{"host", "message", "source"}, drv.indexed("record"))
	// Plugins without search_fields must not trigger an empty CreateIndex.
	require.Empty(t, drv.indexed("audit"))
	require.Equal(t, 1, drv.callCount(), "only the plugin with search_fields is indexed")
}

func TestBuild_SearchFieldRegistrationFailureDoesNotBreakBoot(t *testing.T) {
	// A CreateIndex failure (read-only DB, transient error) must log and
	// continue — boot must not fail just because index creation hit a snag.
	resetForTest()
	Register("record", []byte(`
name: record
search_fields:
  - host
`), newFake("record"))

	drv := newRecordingIndexDB()
	drv.indexErr = errors.New("readonly db")
	host := &nullHost{driver: drv}
	_, _, err := Build(context.Background(), host, nil)
	require.NoError(t, err, "CreateIndex failure must not abort boot")
	// The failure happens on the background goroutine; make sure it really
	// was attempted and swallowed rather than never dispatched.
	require.Eventually(t, func() bool {
		return drv.callCount() == 1
	}, 10*time.Second, 2*time.Millisecond, "CreateIndex was never attempted")
}

// TestBuild_SearchFieldIndexingRunsInBackground is the N3 guard: index
// creation used to run inline, so a first boot against a large database paid
// every CREATE INDEX before the HTTP listener came up. Build must dispatch the
// pass and return.
func TestBuild_SearchFieldIndexingRunsInBackground(t *testing.T) {
	resetForTest()
	Register("record", []byte(`
name: record
search_fields:
  - host
`), newFake("record"))

	drv := newBlockingIndexDriver()
	host := &nullHost{driver: drv}

	// Build runs on its own goroutine only so a regression (inline
	// CreateIndex) fails the test instead of wedging it until the package
	// timeout.
	built := make(chan error, 1)
	go func() {
		_, _, err := Build(context.Background(), host, nil)
		built <- err
	}()
	select {
	case err := <-built:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("Build blocked on CreateIndex: the index pass must run in the background")
	}

	// CreateIndex has been entered and is still parked, which is what proves
	// Build returned without waiting for it.
	select {
	case <-drv.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("CreateIndex was never called")
	}
	require.Empty(t, drv.completed(), "CreateIndex returned before it was released")

	drv.unblock()
	require.Eventually(t, func() bool {
		return len(drv.completed()) == 1
	}, 10*time.Second, 2*time.Millisecond, "released CreateIndex never completed")
	require.Equal(t, []string{"record"}, drv.completed())
}

// TestBuild_BackgroundIndexPassIsAwaitable pins the other half of the
// background dispatch: the goroutine is TRACKED, so shutdown can join it.
//
// Untracked, the pass ran past the end of Build with nothing to wait on it.
// On SQLite and Mongo (neither defers index creation internally) the DDL
// really does execute on that goroutine, so a fast shutdown or a test teardown
// closing the driver underneath it produced a spurious "search-field
// registration failed" warning at best, and a race on the driver's internals
// under -race at worst. Core.StopPlugins now calls WaitBackground.
func TestBuild_BackgroundIndexPassIsAwaitable(t *testing.T) {
	resetForTest()
	Register("record", []byte(`
name: record
search_fields:
  - host
`), newFake("record"))

	drv := newBlockingIndexDriver()
	t.Cleanup(drv.unblock)
	host := &nullHost{driver: drv}
	_, _, err := Build(context.Background(), host, nil)
	require.NoError(t, err)

	select {
	case <-drv.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("CreateIndex was never called")
	}

	// While the pass is parked, WaitBackground must NOT report done.
	parked, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, WaitBackground(parked), context.DeadlineExceeded,
		"WaitBackground must block while the dispatched pass is still running")

	drv.unblock()
	joined, cancelJoin := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelJoin()
	require.NoError(t, WaitBackground(joined))
	require.Equal(t, []string{"record"}, drv.completed(),
		"WaitBackground must not return before the pass has finished")
}
