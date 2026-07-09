// internal/migrate/forward_to_action_test.go
package migrate

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

func TestForwardToAction_ConvertsRow(t *testing.T) {
	drv := newSQLiteDriver(t)
	ctx := context.Background()
	pctx := auth.WithPlatformScope(ctx)

	_, err := drv.Write(pctx, "forward", []db.Document{
		{
			"name":          "central-hub",
			"enabled":       true,
			"endpoint":      "https://hub.example.com/api/v1/alerts",
			"condition":     []any{"=", "severity", "critical"},
			"event_classes": []any{"alerts"},
			"auth": map[string]any{
				"type":  "bearer",
				"token": "s3cr3t",
			},
			"tls_insecure": false,
			"timeout":      30,
			"tenant_id":    snoozetypes.DefaultTenant,
		},
	}, db.WriteOptions{UpdateTime: false})
	require.NoError(t, err)

	require.NoError(t, RunForwardToActionMigration(ctx, drv))

	// Exactly one action doc, shaped as a snoozepeer action.
	actions, _, err := drv.Search(pctx, "action", condition.Cond{}, db.Page{})
	require.NoError(t, err)
	require.Len(t, actions, 1)
	env, ok := actions[0]["action"].(map[string]any)
	require.True(t, ok, "action envelope present")
	require.Equal(t, "snoozepeer", env["selected"])
	sub, ok := env["subcontent"].(map[string]any)
	require.True(t, ok, "subcontent present")
	require.Equal(t, "https://hub.example.com/api/v1/alerts", sub["endpoint"])
	require.NotContains(t, sub, "event_classes", "event_classes has no snoozepeer equivalent and must be dropped")

	// Exactly one notification doc, referencing the converted action by name.
	notifications, _, err := drv.Search(pctx, "notification", condition.Cond{}, db.Page{})
	require.NoError(t, err)
	require.Len(t, notifications, 1)
	actionsField, _ := notifications[0]["actions"].([]any)
	require.Equal(t, []any{"central-hub"}, actionsField)
	require.NotContains(t, notifications[0], "enabled", "enabled:true is the default and need not be stamped")

	// The forward collection is now empty.
	fwd, _, err := drv.Search(pctx, "forward", condition.Cond{}, db.Page{})
	require.NoError(t, err)
	require.Empty(t, fwd)

	// Sentinel recorded.
	done, err := isForwardMigrated(pctx, drv)
	require.NoError(t, err)
	require.True(t, done)
}

func TestForwardToAction_DisabledForwardPreservesDisabledNotification(t *testing.T) {
	drv := newSQLiteDriver(t)
	ctx := context.Background()
	pctx := auth.WithPlatformScope(ctx)

	_, err := drv.Write(pctx, "forward", []db.Document{
		{
			"name":      "disabled-peer",
			"enabled":   false,
			"endpoint":  "https://peer.example.com/api/v1/alerts",
			"tenant_id": snoozetypes.DefaultTenant,
		},
	}, db.WriteOptions{UpdateTime: false})
	require.NoError(t, err)

	require.NoError(t, RunForwardToActionMigration(ctx, drv))

	notifications, _, err := drv.Search(pctx, "notification", condition.Equals("name", "disabled-peer"), db.Page{})
	require.NoError(t, err)
	require.Len(t, notifications, 1)
	require.Equal(t, false, notifications[0]["enabled"])
}

func TestForwardToAction_EmptyIsNoOp(t *testing.T) {
	drv := newSQLiteDriver(t)
	ctx := context.Background()
	pctx := auth.WithPlatformScope(ctx)

	require.NoError(t, RunForwardToActionMigration(ctx, drv))

	actions, _, err := drv.Search(pctx, "action", condition.Cond{}, db.Page{})
	require.NoError(t, err)
	require.Empty(t, actions)

	done, err := isForwardMigrated(pctx, drv)
	require.NoError(t, err)
	require.True(t, done)

	// Second run (sentinel already present) must still be a no-op, no error.
	require.NoError(t, RunForwardToActionMigration(ctx, drv))
	actions2, _, err := drv.Search(pctx, "action", condition.Cond{}, db.Page{})
	require.NoError(t, err)
	require.Empty(t, actions2)
}

// TestForwardToAction_ReRunDoesNotDuplicate proves the doc-comment claim that
// the per-row (tenant_id, name) upserts make a re-run safe even if the sentinel
// guard is bypassed. We convert one row, clear the completion sentinel, re-seed
// the SAME-named forward row, then re-run. The re-run re-converts the row via
// upsert, which must UPDATE the existing action/notification in place rather
// than INSERT duplicates — so exactly one of each remains, with unchanged
// content, and the re-seeded source row is deleted again.
func TestForwardToAction_ReRunDoesNotDuplicate(t *testing.T) {
	drv := newSQLiteDriver(t)
	ctx := context.Background()
	pctx := auth.WithPlatformScope(ctx)

	seedForward := func() {
		_, err := drv.Write(pctx, "forward", []db.Document{
			{
				"name":         "central-hub",
				"enabled":      true,
				"endpoint":     "https://hub.example.com/api/v1/alerts",
				"condition":    []any{"=", "severity", "critical"},
				"tls_insecure": false,
				"timeout":      30,
				"tenant_id":    snoozetypes.DefaultTenant,
			},
		}, db.WriteOptions{UpdateTime: false})
		require.NoError(t, err)
	}

	seedForward()
	require.NoError(t, RunForwardToActionMigration(ctx, drv))

	// Bypass the sentinel guard: clear just the forward-migration marker doc.
	_, err := drv.Delete(pctx, "general", condition.Equals(forwardToActionMarkerField, true), false)
	require.NoError(t, err)
	done, err := isForwardMigrated(pctx, drv)
	require.NoError(t, err)
	require.False(t, done, "sentinel cleared for the re-run")

	// Re-seed the same-named forward row and re-run.
	seedForward()
	require.NoError(t, RunForwardToActionMigration(ctx, drv))

	// Still exactly one action and one notification (upsert updated in place).
	actions, _, err := drv.Search(pctx, "action", condition.Cond{}, db.Page{})
	require.NoError(t, err)
	require.Len(t, actions, 1, "re-run must not duplicate the action")
	env, ok := actions[0]["action"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "snoozepeer", env["selected"])
	sub, ok := env["subcontent"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "https://hub.example.com/api/v1/alerts", sub["endpoint"])

	notifications, _, err := drv.Search(pctx, "notification", condition.Cond{}, db.Page{})
	require.NoError(t, err)
	require.Len(t, notifications, 1, "re-run must not duplicate the notification")
	actionsField, _ := notifications[0]["actions"].([]any)
	require.Equal(t, []any{"central-hub"}, actionsField)

	// The re-seeded source row was deleted again.
	fwd, _, err := drv.Search(pctx, "forward", condition.Cond{}, db.Page{})
	require.NoError(t, err)
	require.Empty(t, fwd)
}
