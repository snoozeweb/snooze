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
)

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

	// 3. Register search_fields with the driver. The SEARCH condition
	//    operator (bare-word search in the UI's SearchBar) compiles to
	//    OR(field ~ /value/i) across the fields registered here; without
	//    this call, the driver's per-collection registry stays empty and
	//    SEARCH matches nothing. CreateIndex also creates a backing DB
	//    index on Postgres for fast substring lookups.
	//
	//    Best-effort: a CreateIndex failure (read-only DB, transient I/O
	//    error) logs a warning and continues. Boot must not fail just
	//    because index creation hit a snag — the SEARCH op will degrade to
	//    "matches nothing" until the operator retries.
	if drv := host.DB(); drv != nil {
		for _, name := range names {
			meta := metas[name]
			if len(meta.SearchFields) == 0 {
				continue
			}
			if err := drv.CreateIndex(ctx, name, meta.SearchFields); err != nil {
				if lg := host.Logger(); lg != nil {
					lg.Warn("plugins.Build: search-field registration failed",
						"plugin", name,
						"fields", meta.SearchFields,
						"err", err)
				}
			}
		}
	}

	// 4. PostInit in deterministic order.
	for _, name := range names {
		if err := all[name].PostInit(ctx, host); err != nil {
			return nil, nil, fmt.Errorf("plugins.Build: post-init %s: %w", name, err)
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
func resetForTest() {
	registry.Range(func(k, _ any) bool {
		registry.Delete(k)
		return true
	})
	registered.Store(0)
	built.Store(false)
}
