// This file holds the one-shot alert-ownership backfill (`snooze-server
// migrate owners`).
//
// Ownership (internal/ownership) is stamped by the operator actions from the
// release that introduced it onward. Records that predate it carry only the
// legacy `acked_by` denormalisation and their comment timeline, so this
// migration derives the ownership keys from those, per tenant:
//
//   - state=ack with acked_by → owned by acked_by, owner_method from the
//     tenant's user directory ("" when the login is unknown or ambiguous),
//     owner_since from the latest human ack comment (else date_epoch);
//   - state=esc with acked_by → unowned, acked_by kept as the previous owner;
//   - state=close → owned by the author of the latest human close comment,
//     when there is one;
//   - anything else → untouched.
//
// Only records with NO `owner` key are considered, and every record the
// migration writes gets one (an escalated record gets an explicit ""), so a
// re-run is a no-op for everything it already handled and never overwrites
// ownership stamped since by the live server. That per-row predicate is the
// idempotency guard — no completion sentinel, so running it again after a
// rolling upgrade (a peer on the old binary kept acking in the meantime) picks
// up the stragglers.

package migrate

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/ownership"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// RunOwnersMigration backfills the ownership keys on every tenant's records
// from acked_by and the comment timeline (see the file comment for the
// rules). Each tenant runs under its own tenant-scoped context, so the users
// and comments it consults are that tenant's. Suspended tenants are migrated
// too: this is a data backfill, not traffic.
func RunOwnersMigration(ctx context.Context, drv db.Driver) error {
	if drv == nil {
		return errors.New("migrate: nil db driver")
	}
	tenants, err := ownersTenants(ctx, drv)
	if err != nil {
		return err
	}
	slog.Info("migrate: starting owners backfill", "tenants", len(tenants))
	total := 0
	for _, id := range tenants {
		n, err := migrateTenantOwners(auth.WithTenant(ctx, id), drv)
		if err != nil {
			return fmt.Errorf("migrate: owners: tenant %s: %w", id, err)
		}
		total += n
	}
	slog.Info("migrate: owners backfill complete", "updated", total)
	return nil
}

// ownersTenants lists the tenant ids to migrate. A database with no tenant
// registry rows (single-tenant, never through the multitenancy migration's
// registry seeding) is migrated as the default tenant alone.
func ownersTenants(ctx context.Context, drv db.Driver) ([]string, error) {
	docs, _, err := drv.Search(auth.WithPlatformScope(ctx), auth.TenantCollection, condition.Cond{}, db.Page{})
	if err != nil {
		return nil, fmt.Errorf("migrate: owners: list tenants: %w", err)
	}
	var ids []string
	for _, d := range docs {
		if id, _ := d["id"].(string); id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		ids = []string{snoozetypes.DefaultTenant}
	}
	return ids, nil
}

// humanComment is the latest human (non-empty `user`) comment of one type on
// one record.
type humanComment struct {
	user string
	date int64
}

// migrateTenantOwners backfills one tenant. tctx must be tenant-scoped.
// Returns the number of records written.
func migrateTenantOwners(tctx context.Context, drv db.Driver) (int, error) {
	candidates := condition.And(
		condition.Not(condition.Exists(ownership.FieldOwner)),
		condition.Or(
			condition.And(
				condition.Or(condition.Equals("state", "ack"), condition.Equals("state", "esc")),
				condition.Exists("acked_by"),
			),
			condition.Equals("state", "close"),
		),
	)
	records, _, err := drv.Search(tctx, "record", candidates, db.Page{})
	if err != nil {
		return 0, fmt.Errorf("search records: %w", err)
	}
	if len(records) == 0 {
		return 0, nil
	}

	methods, err := userMethods(tctx, drv)
	if err != nil {
		return 0, err
	}
	latest, err := latestHumanComments(tctx, drv)
	if err != nil {
		return 0, err
	}

	updated := 0
	for _, rec := range records {
		uid, _ := rec["uid"].(string)
		if uid == "" {
			continue
		}
		patch := ownersPatch(rec, methods, latest[uid])
		if patch == nil {
			continue
		}
		if err := drv.UpdateOne(tctx, "record", uid, patch, false); err != nil {
			return updated, fmt.Errorf("update record %s: %w", uid, err)
		}
		updated++
	}
	return updated, nil
}

// ownersPatch derives one record's ownership patch, or nil when there is
// nothing to backfill. comments holds the record's latest human comment per
// type.
func ownersPatch(rec db.Document, methods map[string][]string, comments map[string]humanComment) db.Document {
	state, _ := rec["state"].(string)
	ackedBy, _ := rec["acked_by"].(string)
	switch {
	case state == "ack" && ackedBy != "":
		since := toInt64(rec["date_epoch"])
		if c, ok := comments["ack"]; ok {
			since = c.date
		}
		return ownership.Take(ackedBy, uniqueMethod(methods, ackedBy), since)
	case state == "esc" && ackedBy != "":
		// The shape an automatic re-escalation leaves: unowned, with the
		// last acknowledger as the ghost.
		patch := db.Document(ownership.Clear(nil))
		patch[ownership.FieldPreviousOwner] = ackedBy
		patch[ownership.FieldPreviousOwnerMethod] = uniqueMethod(methods, ackedBy)
		return patch
	case state == "close":
		c, ok := comments["close"]
		if !ok {
			return nil
		}
		return ownership.Take(c.user, uniqueMethod(methods, c.user), c.date)
	}
	return nil
}

// userMethods maps each login in the tenant's user directory to its auth
// methods (one login may exist under several).
func userMethods(tctx context.Context, drv db.Driver) (map[string][]string, error) {
	users, _, err := drv.Search(tctx, "user", condition.Cond{}, db.Page{})
	if err != nil {
		return nil, fmt.Errorf("search users: %w", err)
	}
	out := map[string][]string{}
	for _, u := range users {
		name, _ := u["name"].(string)
		method, _ := u["method"].(string)
		if name != "" {
			out[name] = append(out[name], method)
		}
	}
	return out, nil
}

// uniqueMethod returns the auth method of login when exactly one user carries
// it, else "" (unknown, or ambiguous across methods). A historical record
// cannot tell which of two same-named users acted, and "" only costs the
// avatar lookup.
func uniqueMethod(methods map[string][]string, login string) string {
	if ms := methods[login]; len(ms) == 1 {
		return ms[0]
	}
	return ""
}

// latestHumanComments indexes the tenant's human ack and close comments by
// record uid and type, keeping the latest of each. A human comment is one with
// a non-empty `user`: the automatic lifecycle comments written by
// aggregaterule and the housekeeper carry none.
func latestHumanComments(tctx context.Context, drv db.Driver) (map[string]map[string]humanComment, error) {
	cond := condition.And(
		condition.Or(condition.Equals("type", "ack"), condition.Equals("type", "close")),
		condition.Exists("user"),
		condition.Not(condition.Equals("user", "")),
	)
	docs, _, err := drv.Search(tctx, "comment", cond, db.Page{})
	if err != nil {
		return nil, fmt.Errorf("search comments: %w", err)
	}
	out := map[string]map[string]humanComment{}
	for _, d := range docs {
		uid, _ := d["record_uid"].(string)
		ctype, _ := d["type"].(string)
		user, _ := d["user"].(string)
		if uid == "" || user == "" {
			continue
		}
		date := toInt64(d["date_epoch"])
		byType := out[uid]
		if byType == nil {
			byType = map[string]humanComment{}
			out[uid] = byType
		}
		if prev, ok := byType[ctype]; !ok || date >= prev.date {
			byType[ctype] = humanComment{user: user, date: date}
		}
	}
	return out, nil
}

// toInt64 reads an epoch out of a document, tolerating the int / int64 /
// float64 shapes the drivers decode numbers into.
func toInt64(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case int32:
		return int64(n)
	case float64:
		return int64(n)
	default:
		return 0
	}
}
