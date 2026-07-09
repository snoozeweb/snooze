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
