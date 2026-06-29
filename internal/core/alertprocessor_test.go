package core

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestMapToRecord_PreserveRawOpt_CopiesExtraKeys verifies the opt-in
// `_preserve_raw` ingest hint: when the posted map carries `_preserve_raw: true`
// alongside foreign (unrecognised) keys, mapToRecord copies every extra key into
// rec.Raw. The sentinel itself must never surface in either rec.Extra or rec.Raw.
func TestMapToRecord_PreserveRawOpt_CopiesExtraKeys(t *testing.T) {
	t.Parallel()

	rec, err := mapToRecord(map[string]any{
		"source":        "my-tool",
		"alertname":     "DiskFull",
		"node":          "web-1",
		"level":         "critical",
		"_preserve_raw": true,
	})
	require.NoError(t, err)

	require.NotNil(t, rec.Raw, "_preserve_raw:true must populate rec.Raw")
	require.Equal(t, "DiskFull", rec.Raw["alertname"])
	require.Equal(t, "web-1", rec.Raw["node"])
	require.Equal(t, "critical", rec.Raw["level"])

	// The sentinel is consumed by mapToRecord and never reaches the record.
	_, inRaw := rec.Raw["_preserve_raw"]
	require.False(t, inRaw, "_preserve_raw must not appear in rec.Raw")
	_, inExtra := rec.Extra["_preserve_raw"]
	require.False(t, inExtra, "_preserve_raw must not appear in rec.Extra")

	// The foreign keys are still visible in Extra so rules can read them.
	require.Equal(t, "web-1", rec.Extra["node"])
	require.Equal(t, "critical", rec.Extra["level"])
	require.Equal(t, "DiskFull", rec.Extra["alertname"])
}

// TestMapToRecord_PreserveRawOpt_FalseIsNoop verifies that a falsy
// `_preserve_raw` is a no-op: rec.Raw stays nil and the foreign keys survive in
// rec.Extra only. The sentinel is still stripped from Extra.
func TestMapToRecord_PreserveRawOpt_FalseIsNoop(t *testing.T) {
	t.Parallel()

	rec, err := mapToRecord(map[string]any{
		"source":        "my-tool",
		"node":          "web-1",
		"_preserve_raw": false,
	})
	require.NoError(t, err)

	require.Nil(t, rec.Raw, "_preserve_raw:false must not populate rec.Raw")
	require.Equal(t, "web-1", rec.Extra["node"], "foreign keys survive in Extra")
	_, inExtra := rec.Extra["_preserve_raw"]
	require.False(t, inExtra, "_preserve_raw must never land in Extra")
}

// TestMapToRecord_NoPreserveRaw_ExtraKeys_NotInRaw verifies the default
// behaviour is unchanged: with no `_preserve_raw` hint, foreign keys live in
// rec.Extra only and rec.Raw stays nil.
func TestMapToRecord_NoPreserveRaw_ExtraKeys_NotInRaw(t *testing.T) {
	t.Parallel()

	rec, err := mapToRecord(map[string]any{
		"source": "my-tool",
		"node":   "web-1",
		"level":  "critical",
	})
	require.NoError(t, err)

	require.Nil(t, rec.Raw, "no _preserve_raw hint must leave rec.Raw nil")
	require.Equal(t, "web-1", rec.Extra["node"])
	require.Equal(t, "critical", rec.Extra["level"])
}

// TestMapToRecord_PreserveRaw_DoesNotClobberExistingRaw verifies that an
// explicitly-posted `raw` object is preserved: `_preserve_raw` merges foreign
// keys in without overwriting keys the sender already placed in raw.
func TestMapToRecord_PreserveRaw_DoesNotClobberExistingRaw(t *testing.T) {
	t.Parallel()

	rec, err := mapToRecord(map[string]any{
		"source":        "my-tool",
		"node":          "web-1",
		"raw":           map[string]any{"existing": float64(1)},
		"_preserve_raw": true,
	})
	require.NoError(t, err)

	require.NotNil(t, rec.Raw)
	require.Equal(t, float64(1), rec.Raw["existing"], "explicit raw entries must be preserved")
	require.Equal(t, "web-1", rec.Raw["node"], "foreign keys are merged into raw")
	_, inRaw := rec.Raw["_preserve_raw"]
	require.False(t, inRaw, "_preserve_raw must not appear in rec.Raw")
}
