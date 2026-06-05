package middleware

import (
	"context"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/pluginimpl/tenant"
)

// DbTenantStatusChecker implements TenantStatusChecker by querying the tenant
// collection under platform scope. It is the concrete wiring used by the
// production Router; tests use stubStatusChecker or nil.
type DbTenantStatusChecker struct {
	driver db.Driver
}

// NewDbTenantStatusChecker creates a checker backed by driver.
func NewDbTenantStatusChecker(driver db.Driver) *DbTenantStatusChecker {
	return &DbTenantStatusChecker{driver: driver}
}

// TenantStatus returns the "status" field of the tenant doc with the given
// tenantID. The query runs under platform scope (the tenant collection is
// global, but we use platform scope for consistency with registry CRUD). A
// missing doc returns ("active", nil) so the ingest path is fail-open on an
// unknown tenant — the driver will then fail-closed on the first scoped write
// if the tenant truly does not exist.
func (c *DbTenantStatusChecker) TenantStatus(ctx context.Context, tenantID string) (string, error) {
	ctx = auth.WithPlatformScope(ctx)
	doc, err := c.driver.GetOne(ctx, tenant.Collection, db.Document{"id": tenantID})
	if err != nil {
		// Fail-open: a transient DB error must not block all ingest.
		return "active", nil
	}
	if doc == nil {
		return "active", nil
	}
	status, _ := doc["status"].(string)
	if status == "" {
		return "active", nil
	}
	return status, nil
}
