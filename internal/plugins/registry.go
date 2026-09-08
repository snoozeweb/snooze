package plugins

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
)

// Factory builds a Plugin instance from its parsed metadata. Factories run
// during Build, after the registry is frozen but before PostInit.
type Factory func(meta Metadata) (Plugin, error)

// entry binds a registered name to its raw metadata + factory.
type entry struct {
	name    string
	metaRaw []byte
	factory Factory
}

var (
	registry   sync.Map // string -> *entry
	registered atomic.Int64
	built      atomic.Bool
	// background tracks the goroutines Build dispatches (today: the
	// search-field registration pass, step 4). Shutdown awaits it via
	// WaitBackground so those goroutines cannot outlive the process' teardown
	// of the things they use — the DB driver above all.
	//
	// Package-scoped rather than returned from Build because the whole
	// registry is: Build runs exactly once per process (it panics on a second
	// call), so there is one set of background work per process too, and
	// threading a WaitGroup through Build's signature would only move the
	// single global somewhere less obvious. resetForTest deliberately does NOT
	// touch it — a WaitGroup cannot be reset while a goroutine still holds a
	// count, and a test's dispatched pass is exactly that.
	background sync.WaitGroup
)

// WaitBackground blocks until every goroutine Build dispatched has returned,
// or ctx expires (returning ctx.Err()). Safe to call when Build never ran, or
// dispatched nothing: the WaitGroup is then at zero and this returns
// immediately.
//
// Callers are expected to cancel the context they passed to Build FIRST —
// that is what bounds this wait. The dispatched work is a sequence of
// driver.CreateIndex calls, and every driver honours cancellation (Postgres
// cancels the pass and closes its maintenance connection, SQLite/Mongo return
// the context error from the in-flight statement), so the wait resolves in one
// cancelled round-trip rather than after the remaining builds.
//
// Without this, the pass ran untracked past the end of Build: a fast shutdown
// or a test teardown could close the driver underneath it, producing a
// spurious "search-field registration failed" warning at best and a data race
// on the driver's internals at worst (the SQLite and Mongo drivers do not
// defer anything internally, so their DDL really is executing on that
// goroutine).
func WaitBackground(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		background.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Register is called from a plugin package's init() to add itself to the
// process-wide plugin registry. It panics on a duplicate name, an empty
// name, or a nil factory: these are configuration bugs that must fail the
// build, not production.
//
// metadataYAML is parsed once and passed to the factory; the factory is
// responsible for storing it (typically via a Metadata field) in the
// concrete plugin value.
func Register(name string, metadataYAML []byte, factory Factory) {
	if name == "" {
		panic("plugins.Register: empty plugin name")
	}
	if factory == nil {
		panic(fmt.Sprintf("plugins.Register: nil factory for %q", name))
	}
	e := &entry{name: name, metaRaw: metadataYAML, factory: factory}
	if _, loaded := registry.LoadOrStore(name, e); loaded {
		panic(fmt.Sprintf("plugins.Register: duplicate plugin name %q", name))
	}
	registered.Add(1)
}

// Registered returns the sorted list of currently registered plugin names.
// The slice is freshly allocated and may be modified by the caller.
func Registered() []string {
	out := make([]string, 0, registered.Load())
	registry.Range(func(k, _ any) bool {
		out = append(out, k.(string))
		return true
	})
	sort.Strings(out)
	return out
}

// New constructs a single registered plugin by name, parsing its metadata and
// running its factory. The returned plugin has NOT had PostInit called, so it
// has no host, no DB and no logger: it is the plugin's own configuration
// surface and nothing more.
//
// Build is the way the server instantiates the full set with wiring; New exists
// for callers that need one plugin's behaviour in isolation — enumerating
// metadata, or exercising a Notifier against a stand-in endpoint.
func New(name string) (Plugin, error) {
	raw, ok := registry.Load(name)
	if !ok {
		return nil, fmt.Errorf("plugins.New: %q is not registered", name)
	}
	e := raw.(*entry)
	meta, err := ParseMetadata(e.metaRaw)
	if err != nil {
		return nil, fmt.Errorf("plugins.New: %s: %w", name, err)
	}
	if meta.Name == "" {
		meta.Name = name
	}
	p, err := e.factory(meta)
	if err != nil {
		return nil, fmt.Errorf("plugins.New: factory for %s: %w", name, err)
	}
	if p == nil {
		return nil, fmt.Errorf("plugins.New: factory for %s returned nil", name)
	}
	return p, nil
}

// Build instantiates every registered plugin via its Factory, calls PostInit
// on each (in lexicographic order — the registry has no dependency graph),
// and returns:
//
//   - all: every plugin keyed by Name().
//   - processors: the ordered Processor slice, filtered and ordered by
//     processOrder. Names in processOrder that do not resolve to a Processor
//     are silently skipped (the configurator is responsible for the contents
//     of process_plugins).
//
// Build may only run once per process; a second call panics. This matches the
// Python codebase's expectation of a single Core lifetime.
//
// Search-field registration and its backing index creation are dispatched to a
// single background goroutine (see step 4) and are therefore NOT complete when
// Build returns. That goroutine is tracked: WaitBackground blocks until it has
// finished, and the shutdown path (Core.StopPlugins) calls it so the pass
// cannot still be issuing DDL while the driver is being closed.
func Build(ctx context.Context, host Host, processOrder []string) (map[string]Plugin, []Processor, error) {
	if !built.CompareAndSwap(false, true) {
		panic("plugins.Build: called twice")
	}

	// 1. Snapshot the registry as a sorted slice for deterministic ordering.
	names := Registered()
	all := make(map[string]Plugin, len(names))

	// 2. Parse metadata + run factories.
	metas := make(map[string]Metadata, len(names))
	for _, name := range names {
		raw, _ := registry.Load(name)
		e := raw.(*entry)
		meta, err := ParseMetadata(e.metaRaw)
		if err != nil {
			return nil, nil, fmt.Errorf("plugins.Build: %s: %w", name, err)
		}
		if meta.Name == "" {
			meta.Name = name
		}
		p, err := e.factory(meta)
		if err != nil {
			return nil, nil, fmt.Errorf("plugins.Build: factory for %s: %w", name, err)
		}
		if p == nil {
			return nil, nil, fmt.Errorf("plugins.Build: factory for %s returned nil", name)
		}
		all[name] = p
		metas[name] = meta
	}

	// 3. PostInit in deterministic order.
	for _, name := range names {
		if err := all[name].PostInit(ctx, host); err != nil {
			return nil, nil, fmt.Errorf("plugins.Build: post-init %s: %w", name, err)
		}
	}

	// 4. Register search_fields with the driver, in ONE background goroutine.
	//
	//    Why register at all: the SEARCH condition operator (bare-word input
	//    in the UI's SearchBar) compiles to OR(field ~* /value/i) across the
	//    fields registered here. With an empty registry SEARCH does not match
	//    nothing — it falls back to regex-scanning the whole serialised
	//    document (postgres `data::text ~* $1`, see db/postgres/dialect.go
	//    Search) — correct, but a guaranteed sequential scan.
	//
	//    Why CreateIndex: it also builds the backing single-field indexes on
	//    all three drivers, so the registered fields are the ones cheap to
	//    filter and sort on. SQLite gets a json_extract expression index
	//    (cheap, local file, no special casing needed); Mongo's builds are
	//    already online; Postgres uses CREATE INDEX CONCURRENTLY, which is
	//    exactly what must not run inline — it deliberately waits out every
	//    transaction older than the build.
	//
	//    Why the background goroutine: this pass used to run inline, so a
	//    first boot against a large existing database paid the full index-build
	//    cost — per collection, per field — before the HTTP listener came up.
	//    The pass is pure optimisation: SEARCH is correct either way, because
	//    every driver's CreateIndex stores its field list BEFORE it touches
	//    the database (postgres/driver.go, sqlite/driver.go and
	//    mongo/driver.go all open with that store), so the field list is
	//    registered the moment the call is entered even though the backing
	//    index is not there yet. What a PENDING pass costs is speed: until
	//    CreateIndex has been reached for a collection, a SEARCH on it still
	//    falls back to the whole-document regex scan, so a query in the first
	//    moments after boot can be slow. It is a single goroutine so N
	//    collections mean N sequential builds, not N concurrent ones fighting
	//    over the same pool.
	//
	//    Ordering, precisely: this dispatch is after step 3, so every PostInit
	//    has already run to completion — including the PostInit hooks that
	//    call CreateIndex themselves (heartbeat indexes its `token` field).
	//    This goroutine is therefore NOT what keeps an inline CONCURRENTLY
	//    build off the boot path, and it could not be: those PostInit calls
	//    happen on the boot critical path with the HTTP listener still down.
	//    The Postgres driver owns that deferral — its CreateIndex registers
	//    the field list, ensures the table, and queues the builds for its own
	//    single worker (see internal/db/postgres/driver.go CreateIndex /
	//    indexWorker) — so EVERY caller is non-blocking, this one included. On
	//    SQLite and Mongo there is nothing to defer: their index creation is
	//    local (a file) or already online, so it runs synchronously inside
	//    this goroutine.
	//
	//    ctx is the process-lifetime boot context: a shutdown mid-build
	//    cancels the remaining work instead of holding the process open. And
	//    because the SQLite and Mongo DDL really does execute here, the
	//    goroutine is registered on the package's `background` WaitGroup,
	//    which Core.StopPlugins awaits (see WaitBackground): an untracked
	//    goroutine issuing DDL against a driver the shutdown path is closing
	//    is a spurious warning at best and a race at worst.
	//
	//    Best-effort: a CreateIndex failure (read-only DB, transient I/O
	//    error, a peer server winning the CONCURRENTLY race) logs a warning
	//    and moves to the next collection.
	if drv := host.DB(); drv != nil {
		type indexJob struct {
			collection string
			fields     []string
		}
		jobs := make([]indexJob, 0, len(names))
		for _, name := range names {
			if fields := metas[name].SearchFields; len(fields) > 0 {
				jobs = append(jobs, indexJob{
					collection: name,
					fields:     append([]string(nil), fields...),
				})
			}
		}
		if len(jobs) > 0 {
			lg := host.Logger()
			background.Add(1)
			go func() {
				defer background.Done()
				for _, job := range jobs {
					if err := drv.CreateIndex(ctx, job.collection, job.fields); err != nil {
						if lg != nil {
							lg.Warn("plugins.Build: search-field registration failed",
								"plugin", job.collection,
								"fields", job.fields,
								"err", err)
						}
					}
				}
			}()
		}
	}

	// 5. Build the ordered Processor slice.
	procs := make([]Processor, 0, len(processOrder))
	for _, name := range processOrder {
		p, ok := all[name]
		if !ok {
			continue
		}
		if proc, ok := p.(Processor); ok {
			procs = append(procs, proc)
		}
	}

	return all, procs, nil
}

// resetForTest wipes the package-global registry. Test-only; never call from
// production code.
//
// It deliberately leaves the `background` WaitGroup alone: a WaitGroup has no
// safe reset while a counted goroutine is still running, and a previous test's
// dispatched index pass may well be. Tests that care about that goroutine call
// WaitBackground instead, which is cumulative across Builds and therefore
// exactly what they want.
func resetForTest() {
	registry.Range(func(k, _ any) bool {
		registry.Delete(k)
		return true
	})
	registered.Store(0)
	built.Store(false)
}
