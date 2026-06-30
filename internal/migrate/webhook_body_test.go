package migrate

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/db"
)

// action is a tiny helper building the {action:{selected,subcontent}} envelope
// the notification plugin stores (internal/pluginimpl/notification.actionDoc).
func action(name, selected string, subcontent map[string]any) db.Document {
	return db.Document{
		"name": name,
		"action": map[string]any{
			"selected":   selected,
			"subcontent": subcontent,
		},
	}
}

func subcontentOf(t *testing.T, doc db.Document) map[string]any {
	t.Helper()
	env, ok := doc["action"].(map[string]any)
	require.True(t, ok, "action envelope present")
	sub, ok := env["subcontent"].(map[string]any)
	require.True(t, ok, "subcontent present")
	return sub
}

func TestRenameWebhookBody(t *testing.T) {
	t.Run("payload only → moved to body, payload dropped", func(t *testing.T) {
		doc := action("wh", "webhook", map[string]any{"url": "http://x", "payload": "TMPL"})
		require.True(t, renameWebhookBody(doc))
		sub := subcontentOf(t, doc)
		require.Equal(t, "TMPL", sub["body"])
		_, hasPayload := sub["payload"]
		require.False(t, hasPayload, "legacy payload key dropped")
		require.Equal(t, "http://x", sub["url"], "sibling fields untouched")
	})

	t.Run("non-empty body wins, payload still dropped", func(t *testing.T) {
		doc := action("wh", "webhook", map[string]any{"body": "REAL", "payload": "STALE"})
		require.True(t, renameWebhookBody(doc))
		sub := subcontentOf(t, doc)
		require.Equal(t, "REAL", sub["body"], "canonical body preserved over stale payload")
		_, hasPayload := sub["payload"]
		require.False(t, hasPayload)
	})

	t.Run("empty body is treated as absent (mirrors runtime fallback)", func(t *testing.T) {
		doc := action("wh", "webhook", map[string]any{"body": "", "payload": "TMPL"})
		require.True(t, renameWebhookBody(doc))
		sub := subcontentOf(t, doc)
		require.Equal(t, "TMPL", sub["body"])
	})

	t.Run("no payload → no change", func(t *testing.T) {
		doc := action("wh", "webhook", map[string]any{"body": "REAL"})
		require.False(t, renameWebhookBody(doc))
	})

	t.Run("non-webhook plugin with a payload key is left alone", func(t *testing.T) {
		doc := action("mailer", "mail", map[string]any{"payload": "do-not-touch"})
		require.False(t, renameWebhookBody(doc))
		sub := subcontentOf(t, doc)
		require.Equal(t, "do-not-touch", sub["payload"], "other plugins may legitimately use payload")
	})

	t.Run("malformed envelopes are ignored", func(t *testing.T) {
		require.False(t, renameWebhookBody(db.Document{"name": "no-action"}))
		require.False(t, renameWebhookBody(db.Document{"action": map[string]any{"selected": "webhook"}}))
	})
}

func TestRunWebhookBodyRenameMigration_SQLite(t *testing.T) {
	drv := newSQLiteDriver(t)
	ctx := context.Background()
	pctx := auth.WithPlatformScope(ctx)

	// Seed: a legacy webhook (payload only), a canonical webhook (body only),
	// a webhook carrying both, a webhook with neither, and a non-webhook that
	// happens to use a `payload` key.
	_, err := drv.Write(pctx, "action", []db.Document{
		action("wh-legacy", "webhook", map[string]any{"url": "http://a", "payload": "LEGACY"}),
		action("wh-canonical", "webhook", map[string]any{"url": "http://b", "body": "CANON"}),
		action("wh-both", "webhook", map[string]any{"body": "KEEP", "payload": "DROP"}),
		action("wh-empty", "webhook", map[string]any{"url": "http://c"}),
		action("mailer", "mail", map[string]any{"payload": "do-not-touch"}),
	}, db.WriteOptions{UpdateTime: false})
	require.NoError(t, err)

	require.NoError(t, RunWebhookBodyRenameMigration(ctx, drv))

	byName := func(name string) db.Document {
		docs, _, err := drv.Search(pctx, "action", condition.Equals("name", name), db.Page{})
		require.NoError(t, err)
		require.Len(t, docs, 1, "exactly one %q action (no duplicate inserted)", name)
		return docs[0]
	}

	// Legacy webhook: payload moved to body, legacy key gone, siblings intact.
	legacy := subcontentOf(t, byName("wh-legacy"))
	require.Equal(t, "LEGACY", legacy["body"])
	require.NotContains(t, legacy, "payload")
	require.Equal(t, "http://a", legacy["url"])

	// Canonical webhook: untouched.
	canon := subcontentOf(t, byName("wh-canonical"))
	require.Equal(t, "CANON", canon["body"])
	require.NotContains(t, canon, "payload")

	// Both present: real body kept, stale payload dropped.
	both := subcontentOf(t, byName("wh-both"))
	require.Equal(t, "KEEP", both["body"])
	require.NotContains(t, both, "payload")

	// Non-webhook: payload preserved.
	mailer := subcontentOf(t, byName("mailer"))
	require.Equal(t, "do-not-touch", mailer["payload"])

	// Sentinel recorded.
	done, err := isWebhookBodyRenamed(pctx, drv)
	require.NoError(t, err)
	require.True(t, done)

	// Idempotent: a second run changes nothing and keeps a single legacy action.
	require.NoError(t, RunWebhookBodyRenameMigration(ctx, drv))
	legacy2 := subcontentOf(t, byName("wh-legacy"))
	require.Equal(t, "LEGACY", legacy2["body"])
	require.NotContains(t, legacy2, "payload")
}
