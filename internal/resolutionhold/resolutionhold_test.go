package resolutionhold

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestStart(t *testing.T) {
	require.Equal(t, map[string]any{FieldUntil: int64(1_000 + 7200), FieldNoted: false},
		Start(1_000, 2*time.Hour))
	require.Equal(t, map[string]any{FieldUntil: int64(0), FieldNoted: false},
		Start(1_000, 0), "a zero hold disables the feature: no deadline")
}

func TestHeld(t *testing.T) {
	analysed := func(status string) map[string]any {
		return map[string]any{"remediation_plan": map[string]any{"status": status}}
	}
	for name, tc := range map[string]struct {
		doc  map[string]any
		now  int64
		held bool
	}{
		"resolved inside the window": {
			doc: map[string]any{FieldUntil: int64(2_000), "agentic": analysed("resolved")}, now: 1_500, held: true,
		},
		"self_resolved inside the window": {
			doc: map[string]any{FieldUntil: float64(2_000), "agentic": analysed("self_resolved")}, now: 1_500, held: true,
		},
		"json.Number deadline": {
			doc: map[string]any{FieldUntil: json.Number("2000"), "agentic": analysed("resolved")}, now: 1_500, held: true,
		},
		"window elapsed": {
			doc: map[string]any{FieldUntil: int64(2_000), "agentic": analysed("resolved")}, now: 2_000,
		},
		"no deadline (automatic close)": {
			doc: map[string]any{FieldUntil: int64(0), "agentic": analysed("resolved")}, now: 1_500,
		},
		"action_required is not a fix": {
			doc: map[string]any{FieldUntil: int64(2_000), "agentic": analysed("action_required")}, now: 1_500,
		},
		"no analysis": {
			doc: map[string]any{FieldUntil: int64(2_000)}, now: 1_500,
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, held := Held(tc.doc, tc.now)
			require.Equal(t, tc.held, held)
		})
	}
}

func TestNoted(t *testing.T) {
	require.True(t, Noted(map[string]any{FieldNoted: true}))
	require.False(t, Noted(map[string]any{}))
}
