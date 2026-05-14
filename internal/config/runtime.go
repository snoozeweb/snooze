package config

import "context"

// RuntimeSettings is the contract for the live-editable counterpart of the
// bootstrap Config. It replaces Python's filelock-protected ``WritableConfig``
// hierarchy. The real implementation lives in the ``settings`` plugin and is
// wired in during Phase 4 of the rewrite; this package only owns the
// interface so that other packages can take a dependency on the abstraction
// today.
//
// Implementations MUST be safe for concurrent use. Updates are expected to
// notify subscribers via Watch so that components like the LDAP backend or
// the notification worker can pick up changes without a restart.
type RuntimeSettings interface {
	// Get returns the value stored under section.key. The bool is false if the
	// key has never been written.
	Get(ctx context.Context, section, key string) (any, bool, error)

	// GetSection unmarshals the full section into dst (typically a pointer to
	// the matching ``schema.*`` struct).
	GetSection(ctx context.Context, section string, dst any) error

	// Set persists a single key/value pair. The implementation is free to
	// reject unknown keys.
	Set(ctx context.Context, section, key string, value any) error

	// Replace overwrites the entire section with values. Useful for the
	// "import settings" use case.
	Replace(ctx context.Context, section string, values map[string]any) error

	// Watch returns a channel that receives a notification each time the
	// section changes. The channel is closed when ctx is cancelled.
	Watch(ctx context.Context, section string) (<-chan RuntimeChange, error)
}

// RuntimeChange describes a single mutation event delivered through
// ``RuntimeSettings.Watch``.
type RuntimeChange struct {
	Section string
	Key     string
	Value   any
}

// NoopRuntimeSettings is a minimal implementation that returns "not found" for
// every read and accepts every write silently. It exists so that callers can
// exercise the Config struct in isolation (in tests, in the migration tool,
// etc.) before the settings plugin is wired in.
type NoopRuntimeSettings struct{}

// Get implements RuntimeSettings.
func (NoopRuntimeSettings) Get(context.Context, string, string) (any, bool, error) {
	return nil, false, nil
}

// GetSection implements RuntimeSettings.
func (NoopRuntimeSettings) GetSection(context.Context, string, any) error { return nil }

// Set implements RuntimeSettings.
func (NoopRuntimeSettings) Set(context.Context, string, string, any) error { return nil }

// Replace implements RuntimeSettings.
func (NoopRuntimeSettings) Replace(context.Context, string, map[string]any) error { return nil }

// Watch implements RuntimeSettings; it returns a closed channel immediately.
func (NoopRuntimeSettings) Watch(ctx context.Context, _ string) (<-chan RuntimeChange, error) {
	ch := make(chan RuntimeChange)
	close(ch)
	return ch, nil
}
