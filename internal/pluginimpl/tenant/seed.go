package tenant

import (
	"context"
	"fmt"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/plugins"
)

// defaultRoles are the roles seeded for every new tenant.
var defaultRoles = []map[string]any{
	{
		"name":        "admin",
		"permissions": []string{"rw_all"},
		"description": "Full access within the tenant",
	},
	{
		"name":        "viewer",
		"permissions": []string{"ro_all"},
		"description": "Read-only access within the tenant",
	},
	{
		"name":        "notifications",
		"permissions": []string{"rw_notification", "ro_all"},
		"description": "Manage notifications within the tenant",
	},
}

// seedDefaultRoles writes the three standard roles into the tenant-scoped role
// collection. The context is scoped to tenantID via auth.WithTenant.
func seedDefaultRoles(ctx context.Context, host plugins.Host, tenantID string) error {
	scopedCtx := auth.WithTenant(ctx, tenantID)
	docs := make([]db.Document, 0, len(defaultRoles))
	for _, r := range defaultRoles {
		d := make(db.Document, len(r))
		for k, v := range r {
			d[k] = v
		}
		docs = append(docs, d)
	}
	_, err := host.DB().Write(scopedCtx, auth.RoleCollection, docs, db.WriteOptions{
		Primary:         []string{"name"},
		DuplicatePolicy: "update",
		UpdateTime:      true,
	})
	if err != nil {
		return fmt.Errorf("tenant: seed roles for %q: %w", tenantID, err)
	}
	return nil
}
