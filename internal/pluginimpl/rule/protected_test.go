package rule

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/modification"
)

func TestGuardWriteRejectsProtectedTarget(t *testing.T) {
	t.Parallel()
	p := &Plugin{}
	cases := []struct {
		name string
		mods []any
	}{
		{"set", []any{[]any{"SET", "agentic", "forged"}}},
		{"set child", []any{[]any{"SET", "agentic.root_cause", "forged"}}},
		{"delete", []any{[]any{"DELETE", "agentic"}}},
		{"array append", []any{[]any{"ARRAY_APPEND", "agentic", "x"}}},
		{"regex sub out field", []any{[]any{"REGEX_SUB", "message", "agentic", ".*", "x"}}},
		{"regex parse capture group", []any{[]any{"REGEX_PARSE", "message", "(?P<agentic>.*)"}}},
		{"buried in a longer list", []any{
			[]any{"SET", "severity", "critical"},
			[]any{"DELETE", "agentic"},
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := p.GuardWrite(context.Background(), "", map[string]any{"modifications": c.mods}, false)
			require.ErrorIs(t, err, modification.ErrProtectedTarget)
		})
	}
}

func TestGuardWriteAllowsOrdinaryModifications(t *testing.T) {
	t.Parallel()
	p := &Plugin{}
	doc := map[string]any{"modifications": []any{
		[]any{"SET", "severity", "critical"},
		[]any{"DELETE", "noisy_field"},
		[]any{"REGEX_PARSE", "message", "(?P<app>[a-z]+) failed"},
	}}
	require.NoError(t, p.GuardWrite(context.Background(), "", doc, false))
}

func TestGuardWriteIgnoresAbsentOrUnparseableModifications(t *testing.T) {
	t.Parallel()
	p := &Plugin{}
	require.NoError(t, p.GuardWrite(context.Background(), "", map[string]any{"name": "no mods"}, false))
	// Malformed input is the loader's error to report, not this guard's.
	require.NoError(t, p.GuardWrite(context.Background(), "",
		map[string]any{"modifications": "not-a-list"}, false))
}

// Not reachable through the HTTP surface today (bulk_update requires a
// DataModel and the rule plugin is a Processor) — asserted so the guard is
// correct the day that changes.
func TestGuardBulkWriteAppliesTheSameCheck(t *testing.T) {
	t.Parallel()
	p := &Plugin{}
	err := p.GuardBulkWrite(context.Background(),
		map[string]any{"modifications": []any{[]any{"SET", "agentic", "forged"}}}, nil, nil)
	require.ErrorIs(t, err, modification.ErrProtectedTarget)

	require.NoError(t, p.GuardBulkWrite(context.Background(),
		map[string]any{"enabled": false}, nil, nil))
}

// The runtime engine is the backstop for a field name only known after
// templating, which the save-time guard cannot see.
func TestApplyRefusesTemplatedProtectedTarget(t *testing.T) {
	t.Parallel()
	view := map[string]any{"target": "agentic", "agentic": map[string]any{"root_cause": "real"}}
	m, err := modification.Parse([]any{"SET", "{{ target }}", "forged"})
	require.NoError(t, err)

	ok, err := modification.Apply(view, m)
	require.ErrorIs(t, err, modification.ErrProtectedTarget)
	require.False(t, ok)
	require.Equal(t, map[string]any{"root_cause": "real"}, view["agentic"], "the stored analysis is untouched")
}

// A rule that mixes a refused modification with legitimate ones still applies
// the legitimate ones — the refusal is scoped to the offending op.
func TestApplyModificationsSkipsOnlyTheProtectedOne(t *testing.T) {
	t.Parallel()
	view := map[string]any{"severity": "warning", "agentic": "stored"}
	mods := parseOrFail(t,
		[]any{"DELETE", "agentic"},
		[]any{"SET", "severity", "critical"},
	)
	applyModifications(view, mods, nil, "mixed")
	require.Equal(t, "stored", view["agentic"])
	require.Equal(t, "critical", view["severity"])
}

func parseOrFail(t *testing.T, raws ...[]any) []modification.Modification {
	t.Helper()
	out := make([]modification.Modification, 0, len(raws))
	for _, raw := range raws {
		m, err := modification.Parse(raw)
		require.NoError(t, err)
		out = append(out, m)
	}
	return out
}

// KV_SET is the one write op that never reaches modification.Apply — it is
// parsed into its own slice and applied by the rule plugin against the host's
// kv store. Without an explicit check it would be the single rule shape able
// to forge a protected field.
func TestGuardWriteRejectsProtectedKVSetTarget(t *testing.T) {
	t.Parallel()
	p := &Plugin{}
	err := p.GuardWrite(context.Background(), "", map[string]any{
		"modifications": []any{[]any{"KV_SET", "hosts", "host", "agentic"}},
	}, false)
	require.ErrorIs(t, err, modification.ErrProtectedTarget)

	// A child path is protected too, and an ordinary out_field still saves.
	require.ErrorIs(t, p.GuardWrite(context.Background(), "", map[string]any{
		"modifications": []any{[]any{"KV_SET", "hosts", "host", "agentic.root_cause"}},
	}, false), modification.ErrProtectedTarget)
	require.NoError(t, p.GuardWrite(context.Background(), "", map[string]any{
		"modifications": []any{[]any{"KV_SET", "hosts", "host", "owner_team"}},
	}, false))
}

// The runtime half: a rule stored before the field became protected must not
// still write it on the next alert.
func TestApplyKVSetsSkipsProtectedOutField(t *testing.T) {
	t.Parallel()
	p := &Plugin{}
	view := map[string]any{"host": "srv-1", "agentic": "stored analysis"}
	p.applyKVSets(context.Background(), view, []kvSet{
		{Dict: "hosts", Key: "host", OutField: "agentic"},
	})
	require.Equal(t, "stored analysis", view["agentic"])
}
