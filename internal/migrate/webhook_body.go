// This file holds the one-shot webhook `payload`→`body` rename migration.
//
// The Python-era webhook plugin (src/snooze/plugins/core/webhook) stored the
// request-body template under the action_form key `payload`. The Go port
// standardised on `body` (see internal/pluginimpl/webhook). Actions ported
// from 1.x therefore keep the template under `payload`, which renders the
// editor's Body field empty over real data and forces a permanent runtime
// fallback in the dispatcher. This migration rewrites the stored documents
// once so the data matches the canonical schema, after which both the editor
// and the dispatcher need only know about `body`.

package migrate

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/db"
)

// webhookBodyRenameMarkerField is the idempotency sentinel for this migration.
// It lives in the same "general" collection as the multitenancy sentinel
// (migrationMarkerCollection); the two markers are independent docs.
const webhookBodyRenameMarkerField = "webhook_body_rename_v1"

// isWebhookBodyRenamed reports whether the rename migration has already run.
// ctx must carry WithPlatformScope: `general` is tenant-scoped, and the
// sentinel is written without a tenant_id, so only a platform-scoped read sees
// it without tripping the driver's fail-closed tenant guard.
func isWebhookBodyRenamed(ctx context.Context, drv db.Driver) (bool, error) {
	docs, _, err := drv.Search(ctx, migrationMarkerCollection, condition.Cond{}, db.Page{})
	if err != nil {
		return false, fmt.Errorf("migrate: check webhook-body sentinel: %w", err)
	}
	for _, d := range docs {
		if b, _ := d[webhookBodyRenameMarkerField].(bool); b {
			return true, nil
		}
	}
	return false, nil
}

// writeWebhookBodyRenameSentinel stamps the migration-complete marker. It
// upserts uid-based (no Primary), but the sentinel guard means it only ever
// runs once, so no duplicate accumulates.
func writeWebhookBodyRenameSentinel(ctx context.Context, drv db.Driver) error {
	_, err := drv.Write(ctx, migrationMarkerCollection, []db.Document{
		{webhookBodyRenameMarkerField: true},
	}, db.WriteOptions{UpdateTime: true})
	if err != nil {
		return fmt.Errorf("migrate: write webhook-body sentinel: %w", err)
	}
	return nil
}

// renameWebhookBody normalises a single action document in place and reports
// whether it changed. Only webhook actions are touched — other plugins may
// legitimately carry a `payload` subcontent key, so scoping by the selected
// notifier is a correctness requirement, not just an optimisation.
//
// Move semantics mirror the dispatcher's former runtime fallback
// (configFromPayload): an empty-string `body` counted as absent, so `payload`
// wins only when `body` carries no real template. The legacy key is always
// dropped, so a second pass over the same data is a no-op.
func renameWebhookBody(doc db.Document) bool {
	env, ok := doc["action"].(map[string]any)
	if !ok {
		return false
	}
	if sel, _ := env["selected"].(string); sel != "webhook" {
		return false
	}
	sub, ok := env["subcontent"].(map[string]any)
	if !ok {
		return false
	}
	payload, hasPayload := sub["payload"]
	if !hasPayload {
		return false
	}
	if body, _ := sub["body"].(string); body == "" {
		sub["body"] = payload
	}
	delete(sub, "payload")
	return true
}

// RunWebhookBodyRenameMigration renames every webhook action's legacy `payload`
// subcontent key to the canonical `body` key and drops the legacy key. It is
// idempotent twice over: a completion sentinel short-circuits re-runs, and the
// rename itself is a no-op once no `payload` keys remain. It runs under platform
// scope so it sees and rewrites every tenant's actions in one pass.
//
// Run it after RunMultitenancyMigration on a pre-multitenancy database: by then
// action docs carry tenant_id, which the read-modify-ReplaceOne cycle preserves
// verbatim (ReplaceOne writes back the full document, including tenant_id, and
// platform scope adds no predicate of its own). On a multitenancy-native
// database the ordering is moot.
func RunWebhookBodyRenameMigration(ctx context.Context, drv db.Driver) error {
	if drv == nil {
		return errors.New("migrate: nil db driver")
	}
	// Platform scope bypasses the driver's tenant injection so the migration
	// reads and rewrites across all tenants.
	pctx := auth.WithPlatformScope(ctx)

	done, err := isWebhookBodyRenamed(pctx, drv)
	if err != nil {
		return err
	}
	if done {
		slog.Info("migrate: webhook body rename already complete, skipping")
		return nil
	}

	slog.Info("migrate: starting webhook body rename")

	docs, _, err := drv.Search(pctx, "action", condition.Cond{}, db.Page{})
	if err != nil {
		return fmt.Errorf("migrate: list actions: %w", err)
	}

	renamed := 0
	for _, doc := range docs {
		if !renameWebhookBody(doc) {
			continue
		}
		uid, _ := doc["uid"].(string)
		if uid == "" {
			slog.Warn("migrate: webhook action without uid, skipping rename", "name", doc["name"])
			continue
		}
		// Full-document replace (not a merge Write) so the dropped `payload`
		// key is truly gone from storage on every backend. updateTime=false
		// keeps the action's timestamps untouched — this is a transparent
		// format migration, not a user edit.
		if _, err := drv.ReplaceOne(pctx, "action", db.Document{"uid": uid}, doc, false); err != nil {
			return fmt.Errorf("migrate: rewrite action %s: %w", uid, err)
		}
		renamed++
	}

	if err := writeWebhookBodyRenameSentinel(pctx, drv); err != nil {
		return err
	}
	slog.Info("migrate: webhook body rename complete", "actions_renamed", renamed)
	return nil
}
