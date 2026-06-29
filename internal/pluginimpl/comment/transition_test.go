package comment

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// allowedMoves and rejectedMoves transcribe the transition table from Plan 05's
// Design section. Each (currentState, action) pair is asserted independently so
// a single regressed cell is named in the failure output.
var (
	allowedMoves = []struct{ state, action string }{
		// currentState "" (fresh/open): ack ✓, close ✓
		{"", "ack"},
		{"", "close"},
		// currentState "ack": close ✓, open ✓, esc ✓
		{"ack", "close"},
		{"ack", "open"},
		{"ack", "esc"},
		// currentState "esc": ack ✓, close ✓, open ✓
		{"esc", "ack"},
		{"esc", "close"},
		{"esc", "open"},
		// currentState "close": open ✓
		{"close", "open"},
		// currentState "open": ack ✓, close ✓
		{"open", "ack"},
		{"open", "close"},
	}
	rejectedMoves = []struct{ state, action string }{
		// currentState "" (fresh/open): open ✗, esc ✗
		{"", "open"},
		{"", "esc"},
		// currentState "ack": ack ✗
		{"ack", "ack"},
		// currentState "esc": esc ✗
		{"esc", "esc"},
		// currentState "close": ack ✗, close ✗, esc ✗
		{"close", "ack"},
		{"close", "close"},
		{"close", "esc"},
		// currentState "open": open ✗, esc ✗
		{"open", "open"},
		{"open", "esc"},
	}
)

func TestValidateTransition_AllowedMoves(t *testing.T) {
	for _, m := range allowedMoves {
		t.Run(m.state+"_"+m.action, func(t *testing.T) {
			require.NoError(t, ValidateTransition(m.state, m.action),
				"transition %q -> %q must be allowed", m.state, m.action)
		})
	}
}

func TestValidateTransition_RejectedMoves(t *testing.T) {
	for _, m := range rejectedMoves {
		t.Run(m.state+"_"+m.action, func(t *testing.T) {
			err := ValidateTransition(m.state, m.action)
			require.Error(t, err,
				"transition %q -> %q must be rejected", m.state, m.action)
			require.True(t, errors.Is(err, ErrInvalidTransition),
				"rejected transition must wrap ErrInvalidTransition, got %v", err)
		})
	}
}

func TestValidateTransition_UnknownStatePassesThrough(t *testing.T) {
	// A currentState not in the table (e.g. a future extension) must fail open.
	require.NoError(t, ValidateTransition("frobnicate", "ack"))
	require.NoError(t, ValidateTransition("frobnicate", "close"))
}

func TestValidateTransition_NonStateAction(t *testing.T) {
	// An action outside {ack,close,open,esc} is not a state transition; the
	// guard only governs known actions, so this must pass through.
	require.NoError(t, ValidateTransition("ack", "note"))
	require.NoError(t, ValidateTransition("", "note"))
	require.NoError(t, ValidateTransition("close", ""))
}
