package core

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/internal/syncer"
)

// fakeProcessorWithDeps is a fakeProcessor that also declares reload
// dependencies (the syncer.ReloadDeps contract).
type fakeProcessorWithDeps struct {
	fakeProcessor
	deps []string
}

func (f *fakeProcessorWithDeps) ReloadCollections() []string { return f.deps }

// TestPluggableShim_ForwardsReloadCollections guards the integration seam: the
// syncer type-asserts its Pluggables to ReloadDeps, but boot.go wraps every
// plugin in pluggableShim. If the shim doesn't forward ReloadCollections, the
// notification plugin's `action` dependency is silently dropped and action
// edits don't propagate to the running dispatcher.
func TestPluggableShim_ForwardsReloadCollections(t *testing.T) {
	t.Parallel()

	// Compile-time: the shim must satisfy the syncer's dependency interface.
	var _ syncer.ReloadDeps = pluggableShim{}

	withDeps := &fakeProcessorWithDeps{
		fakeProcessor: fakeProcessor{name: "notification"},
		deps:          []string{"action"},
	}
	shim := pluggableShim{name: "notification", plugin: withDeps}
	require.Equal(t, []string{"action"}, shim.ReloadCollections(),
		"shim must forward the underlying plugin's reload dependencies")

	// A plugin that declares no dependencies yields nil (no extra subscriptions).
	plain := pluggableShim{name: "rule", plugin: &fakeProcessor{name: "rule"}}
	require.Nil(t, plain.ReloadCollections())
}

func TestFilterOptionalPlugins_DropsDefaultDisabled(t *testing.T) {
	t.Parallel()
	all := map[string]plugins.Plugin{
		"rule":    &fakeProcessor{name: "rule"},
		"patlite": &fakeProcessor{name: "patlite"},
	}
	out := filterOptionalPlugins(all, nil)
	require.Contains(t, out, "rule")
	require.NotContains(t, out, "patlite",
		"patlite is optional and must be hidden when not in the enabled list")
}

func TestFilterOptionalPlugins_KeepsExplicitlyEnabled(t *testing.T) {
	t.Parallel()
	all := map[string]plugins.Plugin{
		"rule":    &fakeProcessor{name: "rule"},
		"patlite": &fakeProcessor{name: "patlite"},
	}
	out := filterOptionalPlugins(all, []string{"patlite"})
	require.Contains(t, out, "rule")
	require.Contains(t, out, "patlite",
		"patlite must remain when listed in enabled_optional_plugins")
}

func TestFilterOptionalPlugins_UnknownNameInEnabledIsIgnored(t *testing.T) {
	t.Parallel()
	all := map[string]plugins.Plugin{"rule": &fakeProcessor{name: "rule"}}
	out := filterOptionalPlugins(all, []string{"ghost"})
	require.Len(t, out, 1)
	require.Contains(t, out, "rule")
}
