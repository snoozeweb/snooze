package snoozetypes

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestRecord_HashRoundTrips guards the snooze-server → snooze-teams wire hop.
// Hash MUST be a typed field, not buried in Extra, because Extra carries
// `json:"-"` and is silently dropped by encoding/json — that is the regression
// that left every Teams alert's host name unlinked: the daemon received the
// alert JSON, the hash field had no typed home, recordWebURL returned "" and
// hostText fell back to bare text with no Markdown link.
func TestRecord_HashRoundTrips(t *testing.T) {
	in := Record{
		Host: "db-1.example.com",
		Hash: "0123456789abcdef0123456789abcdef",
	}
	raw, err := json.Marshal(in)
	require.NoError(t, err)
	require.Contains(t, string(raw), `"hash":"0123456789abcdef0123456789abcdef"`,
		"Hash must serialize so snooze-teams receives it on the wire")

	var out Record
	require.NoError(t, json.Unmarshal(raw, &out))
	require.Equal(t, in.Hash, out.Hash,
		"Hash must survive an Unmarshal so the daemon can build the host link URL")
}
