package comment

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

func TestValidate_Source(t *testing.T) {
	p := &Plugin{}
	require.NoError(t, p.Validate(map[string]any{"message": "x", "source": "snooze-skill"}))
	require.NoError(t, p.Validate(map[string]any{"message": "x"}), "source is optional")
	require.ErrorContains(t, p.Validate(map[string]any{"message": "x", "source": 42}), "must be a string")
	require.ErrorContains(t, p.Validate(map[string]any{"message": "x", "source": strings.Repeat("a", 65)}), "at most 64")
	require.ErrorContains(t, p.Validate(map[string]any{"message": "x", "source": "a\x00b"}), "NUL")
}

// The tool tag is the client's to set; TransformWrite only makes `user`
// authoritative and leaves `source` as sent.
func TestTransformWriteKeepsSource(t *testing.T) {
	p := &Plugin{}
	doc := map[string]any{"message": "x", "source": "snooze-skill"}
	require.NoError(t, p.TransformWrite(auth.WithClaims(context.Background(), snoozetypes.Claims{Subject: "alice", Method: "local"}), doc))
	require.Equal(t, "alice", doc["user"])
	require.Equal(t, "snooze-skill", doc["source"])
}
