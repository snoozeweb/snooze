package dbtest

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/japannext/snooze/internal/db"
)

//go:embed testdata/*.json
var fixtureFS embed.FS

// Set maps a collection name to the records it should contain after a Load.
type Set map[string][]map[string]any

// LoadEmbedded parses an embedded fixture JSON file into a Set.
func LoadEmbedded(t *testing.T, name string) Set {
	t.Helper()
	data, err := fixtureFS.ReadFile("testdata/" + name + ".json")
	require.NoError(t, err)
	out := Set{}
	require.NoError(t, json.Unmarshal(data, &out))
	return out
}

// Load drops every collection in s then writes its records back fresh. Use
// this between subtests to keep a clean slate.
func Load(t *testing.T, drv db.Driver, s Set) {
	t.Helper()
	ctx := context.Background()
	for collection, docs := range s {
		require.NoError(t, drv.Drop(ctx, collection))
		if len(docs) == 0 {
			continue
		}
		out := make([]db.Document, 0, len(docs))
		for _, d := range docs {
			out = append(out, d)
		}
		_, err := drv.Write(ctx, collection, out, db.WriteOptions{UpdateTime: false})
		require.NoError(t, err, fmt.Sprintf("seed %s", collection))
	}
}
