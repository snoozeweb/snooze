package core

import (
	"context"
	"errors"
	"fmt"

	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/housekeeper"
)

// generalCollection holds the bootstrap marker doc.
const generalCollection = "general"

// roleCollection is the storage collection holding role documents.
const roleCollection = "role"

// aggregateRuleCollection is the storage collection for aggregate rules.
const aggregateRuleCollection = "aggregaterule"

// bootstrapMarkerField is the field name on a single "general" doc that, when
// set, indicates a prior bootstrap. Matches Python's "init_db" flag.
const bootstrapMarkerField = "init_db"

// defaultRoles are the canonical RBAC seed roles. adminGroup, when non-empty,
// is added to the admin role's groups[] so an SSO/LDAP group with that value
// maps to admin via the RBAC resolver (used for OIDC admin->admin mapping).
func defaultRoles(adminGroup string) []db.Document {
	adminGroups := []string{}
	if adminGroup != "" {
		adminGroups = []string{adminGroup}
	}
	return []db.Document{
		{
			"name":        "admin",
			"permissions": []string{"rw_all"},
			"groups":      adminGroups,
		},
		{
			"name":        "viewer",
			"permissions": []string{"ro_all"},
		},
		{
			"name": "notifications",
			// ro_notificationlog is what makes the Deliveries surface readable:
			// the delivery history lives in its own `notificationlog`
			// collection, so rw_notification alone gets a 403 on every
			// GET /api/v1/notificationlog. Keep in lock-step with the
			// per-tenant seed in internal/pluginimpl/tenant/seed.go and with
			// backfillNotificationsRole below.
			"permissions": []string{"rw_notification", notificationLogReadPerm},
		},
	}
}

// notificationLogReadPerm is the implicit read grant for the `notificationlog`
// collection (plugins derive ro_<plugin> / rw_<plugin> from the plugin name).
const notificationLogReadPerm = "ro_notificationlog"

// notificationsRoleName is the seeded role that owns notification management.
const notificationsRoleName = "notifications"

// defaultAggregateRules are the canonical aggregate-rule seed values.
func defaultAggregateRules() []db.Document {
	return []db.Document{
		{
			"name":      "Host and Message",
			"fields":    []string{"host", "message"},
			"condition": []any{},
			"throttle":  int64(900),
		},
	}
}

// BootstrapDB seeds the default roles, the root user, and the default
// aggregate rule. The seeding is idempotent: a marker document in the
// "general" collection prevents subsequent runs from re-writing the seeds.
//
// EnsureRoot (in package auth) is invoked separately by the boot sequence;
// BootstrapDB intentionally does not write user rows so the two responsibilities
// stay decoupled.
func BootstrapDB(ctx context.Context, drv db.Driver, adminGroup string) error {
	if drv == nil {
		return errors.New("bootstrap_db: nil driver")
	}

	// Already bootstrapped? Skip.
	docs, _, err := drv.Search(ctx, generalCollection, condition.Cond{}, db.Page{})
	if err == nil && len(docs) > 0 {
		for _, d := range docs {
			if v, ok := d[bootstrapMarkerField]; ok {
				if b, ok := v.(bool); ok && b {
					return nil
				}
			}
		}
	}

	// 1. Roles.
	if _, err := drv.Write(ctx, roleCollection, defaultRoles(adminGroup), db.WriteOptions{
		Primary:    []string{"name"},
		UpdateTime: true,
	}); err != nil {
		return fmt.Errorf("bootstrap_db: write roles: %w", err)
	}

	// 2. Default aggregate rule.
	if _, err := drv.Write(ctx, aggregateRuleCollection, defaultAggregateRules(), db.WriteOptions{
		Primary:    []string{"name"},
		UpdateTime: true,
	}); err != nil {
		return fmt.Errorf("bootstrap_db: write aggregaterule: %w", err)
	}

	// 3. Marker doc.
	if _, err := drv.Write(ctx, generalCollection, []db.Document{{
		bootstrapMarkerField: true,
	}}, db.WriteOptions{UpdateTime: true}); err != nil {
		return fmt.Errorf("bootstrap_db: write marker: %w", err)
	}

	return nil
}

// BackfillNotificationsRolePerms grants ro_notificationlog to any existing
// `notifications` role that predates the delivery log.
//
// It exists because the seeds above are one-shot: BootstrapDB short-circuits
// on the `init_db` marker, and the per-tenant seed only runs at tenant
// creation, so an install that booted before `notificationlog` existed keeps a
// notifications role with `rw_notification` alone — and every operator holding
// only that role gets a 403 on the Deliveries tab.
//
// Idempotent and conservative: a role that already carries the grant, or a
// catch-all (ro_all / rw_all) that subsumes it, is left untouched, so repeat
// boots are no-ops. Note it re-adds the grant on every boot to a role that had
// it removed by hand — renaming the role (or dropping it) is how an operator
// opts out. That trade-off is deliberate: the alternative is a role named
// "notifications" whose Deliveries tab silently 403s.
//
// Runs across every active tenant; a per-tenant failure aborts the sweep and
// is reported to the caller, which logs rather than failing boot.
func BackfillNotificationsRolePerms(ctx context.Context, drv db.Driver) error {
	if drv == nil {
		return errors.New("bootstrap_db: nil driver")
	}
	return housekeeper.ForEachTenant(ctx, drv, func(tctx context.Context, tenantID string) error {
		docs, _, err := drv.Search(tctx, roleCollection,
			condition.Equals("name", notificationsRoleName), db.Page{})
		if err != nil {
			return fmt.Errorf("bootstrap_db: backfill notifications role for %q: %w", tenantID, err)
		}
		for _, doc := range docs {
			uid, _ := doc["uid"].(string)
			if uid == "" {
				continue
			}
			perms := stringList(doc["permissions"])
			if hasAny(perms, notificationLogReadPerm, "ro_all", "rw_all") {
				continue
			}
			next := make([]any, 0, len(perms)+1)
			for _, p := range perms {
				next = append(next, p)
			}
			next = append(next, notificationLogReadPerm)
			if _, err := drv.SetFields(tctx, roleCollection,
				db.Document{"permissions": next}, condition.Equals("uid", uid)); err != nil {
				return fmt.Errorf("bootstrap_db: backfill notifications role for %q: %w", tenantID, err)
			}
		}
		return nil
	})
}

// stringList coerces a stored JSON array (which decodes as []any) or a
// still-typed []string into a plain []string.
func stringList(v any) []string {
	switch typed := v.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

// hasAny reports whether list contains any of the wanted values.
func hasAny(list []string, wanted ...string) bool {
	for _, item := range list {
		for _, w := range wanted {
			if item == w {
				return true
			}
		}
	}
	return false
}
