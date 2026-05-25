package core

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/plugins"
)

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
