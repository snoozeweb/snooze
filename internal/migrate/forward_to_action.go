// This file holds the one-shot forward->action/notification migration.
//
// Alert federation used to live in a dedicated `forward` collection (a
// DataModel+Processor plugin). It is now expressed as a `snoozepeer` Notifier
// dispatched by the notification engine. This migration rewrites each stored
// forward destination into an `action` (selected=snoozepeer) plus a
// `notification` entry carrying the destination's condition, then deletes the
// converted forward doc. It is sentinel-guarded and idempotent, and a no-op
// when the forward collection is empty or absent.

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

// forwardToActionMarkerField is the idempotency sentinel for this migration.
// It lives in the same "general" collection as the other migration markers
// (migrationMarkerCollection); each marker is an independent doc.
const forwardToActionMarkerField = "forward_to_action_v1"

// isForwardMigrated reports whether the forward->action migration has already
// run. ctx must carry WithPlatformScope: `general` is tenant-scoped, and the
// sentinel is written without a tenant_id, so only a platform-scoped read sees
// it without tripping the driver's fail-closed tenant guard.
func isForwardMigrated(ctx context.Context, drv db.Driver) (bool, error) {
	docs, _, err := drv.Search(ctx, migrationMarkerCollection, condition.Cond{}, db.Page{})
	if err != nil {
		return false, fmt.Errorf("migrate: check forward sentinel: %w", err)
	}
	for _, d := range docs {
		if b, _ := d[forwardToActionMarkerField].(bool); b {
			return true, nil
		}
	}
	return false, nil
}

// writeForwardMigratedSentinel stamps the migration-complete marker. It
// upserts uid-based (no Primary), but the sentinel guard means it only ever
// runs once, so no duplicate accumulates.
func writeForwardMigratedSentinel(ctx context.Context, drv db.Driver) error {
	_, err := drv.Write(ctx, migrationMarkerCollection, []db.Document{
		{forwardToActionMarkerField: true},
	}, db.WriteOptions{UpdateTime: true})
	if err != nil {
		return fmt.Errorf("migrate: write forward sentinel: %w", err)
	}
	return nil
}

// fwdStr reads a string field off a forward doc, defaulting to "".
func fwdStr(doc db.Document, k string) string {
	v, _ := doc[k].(string)
	return v
}

// RunForwardToActionMigration converts every `forward` row into an
// action+notification pair and deletes the source row. Idempotent twice over:
// a completion sentinel short-circuits re-runs, and per-row upserts key on
// (tenant_id, name). Runs under platform scope so it processes all tenants in
// one pass.
//
// The `forward` collection is not in TenantScopedCollections, so a forward row
// that predates the multitenancy migration may carry no tenant_id at all; the
// converted action/notification docs then also carry no tenant_id, mirroring
// every other collection's pre-migration shape (a later run of the
// multitenancy backfill, or the tenant-scoping fail-closed guard, handles that
// the same way it handles any other legacy untenanted row). This is a
// best-effort defensive path: srv-snooze production, and every other known
// deployment, does not have any forward rows.
func RunForwardToActionMigration(ctx context.Context, drv db.Driver) error {
	if drv == nil {
		return errors.New("migrate: nil db driver")
	}
	// Platform scope bypasses the driver's tenant injection so the migration
	// reads and rewrites across all tenants in one pass.
	pctx := auth.WithPlatformScope(ctx)

	done, err := isForwardMigrated(pctx, drv)
	if err != nil {
		return err
	}
	if done {
		slog.Info("migrate: forward->action migration already complete, skipping")
		return nil
	}

	slog.Info("migrate: starting forward->action migration")

	// A missing `forward` collection (the production srv-snooze case, and every
	// other known deployment) is not an error: Search on a nonexistent
	// collection returns an empty result set, and the loop below is then a
	// no-op that still stamps the sentinel so the check above short-circuits
	// on every subsequent boot.
	fwd, _, err := drv.Search(pctx, "forward", condition.Cond{}, db.Page{})
	if err != nil {
		return fmt.Errorf("migrate: list forward destinations: %w", err)
	}

	converted := 0
	for _, d := range fwd {
		name := fwdStr(d, "name")
		if name == "" {
			slog.Warn("migrate: forward row without name, skipping")
			continue
		}
		tenantID := fwdStr(d, "tenant_id")

		// event_classes has no snoozepeer equivalent (there is no
		// action/delete federation surface today) and is intentionally
		// dropped rather than carried into subcontent.
		subcontent := db.Document{
			"endpoint":     d["endpoint"],
			"tls_insecure": d["tls_insecure"],
			"timeout":      d["timeout"],
		}
		if a, ok := d["auth"].(map[string]any); ok {
			subcontent["auth"] = a
		}

		action := db.Document{
			"name": name,
			"action": map[string]any{
				"selected":   "snoozepeer",
				"subcontent": subcontent,
			},
		}
		notification := db.Document{
			"name":      name,
			"condition": d["condition"],
			"actions":   []any{name},
		}
		if en, ok := d["enabled"].(bool); ok && !en {
			notification["enabled"] = false
		}
		if tenantID != "" {
			action["tenant_id"] = tenantID
			notification["tenant_id"] = tenantID
		}

		// Primary explicitly includes tenant_id: under platform scope the
		// driver does not fold a tenant predicate into the upsert lookup, so
		// omitting it here would let two tenants' same-named action/
		// notification collide. Mirrors migrate.go's grantRootPlatformAdmin
		// convention (Primary: []string{"tenant_id", "name", ...}).
		if _, err := drv.Write(pctx, "action", []db.Document{action},
			db.WriteOptions{Primary: []string{"tenant_id", "name"}, UpdateTime: true}); err != nil {
			return fmt.Errorf("migrate: write action %q: %w", name, err)
		}
		if _, err := drv.Write(pctx, "notification", []db.Document{notification},
			db.WriteOptions{Primary: []string{"tenant_id", "name"}, UpdateTime: true}); err != nil {
			return fmt.Errorf("migrate: write notification %q: %w", name, err)
		}
		converted++
	}

	if len(fwd) > 0 {
		// force=true: an unconditional delete of every remaining forward row
		// is the point (the collection is retired), and the sqlite driver
		// otherwise refuses an empty-condition delete as a safety guard.
		if _, err := drv.Delete(pctx, "forward", condition.Cond{}, true); err != nil {
			return fmt.Errorf("migrate: delete converted forward rows: %w", err)
		}
	}

	if err := writeForwardMigratedSentinel(pctx, drv); err != nil {
		return err
	}
	slog.Info("migrate: forward->action migration complete", "converted", converted)
	return nil
}
