package core

import (
	"context"
	"errors"
	"fmt"

	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/db"
)

// generalCollection holds the bootstrap marker doc.
const generalCollection = "general"

// roleCollection is the storage collection holding role documents.
const roleCollection = "role"

// aggregateRuleCollection is the storage collection for aggregate rules.
const aggregateRuleCollection = "aggregaterule"

// bootstrapMarkerField is the field name on a single “general“ doc that, when
// set, indicates a prior bootstrap. Matches Python's “init_db“ flag.
const bootstrapMarkerField = "init_db"

// defaultRoles are the canonical RBAC seed roles. Names mirror the Python
// codebase; “viewer“ replaces Python's “user“ to make the read-only intent
// explicit (the legacy name is also seeded for backwards compatibility).
func defaultRoles() []db.Document {
	return []db.Document{
		{
			"name":        "admin",
			"permissions": []string{"rw_all"},
		},
		{
			"name":        "viewer",
			"permissions": []string{"ro_all"},
		},
		{
			"name":        "notifications",
			"permissions": []string{"rw_notification"},
		},
	}
}

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
// “general“ collection prevents subsequent runs from re-writing the seeds.
//
// EnsureRoot (in package auth) is invoked separately by the boot sequence;
// BootstrapDB intentionally does not write user rows so the two responsibilities
// stay decoupled.
func BootstrapDB(ctx context.Context, drv db.Driver) error {
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
	if _, err := drv.Write(ctx, roleCollection, defaultRoles(), db.WriteOptions{
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
