// internal/api/middleware/tenant_checker_test.go
package middleware_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/api/middleware"
	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// stubDriver is a minimal db.Driver stub for TenantStatusChecker tests.
type stubDriver struct {
	db.Driver // embed to satisfy interface; unused methods panic
	docs      map[string]db.Document
}

func (s *stubDriver) GetOne(_ context.Context, collection string, filter db.Document) (db.Document, error) {
	id, _ := filter["id"].(string)
	key := collection + "/" + id
	doc, ok := s.docs[key]
	if !ok {
		return nil, nil
	}
	return doc, nil
}

func (s *stubDriver) Search(_ context.Context, _ string, _ condition.Cond, _ db.Page) ([]db.Document, int, error) {
	return nil, 0, nil
}

func TestDbTenantStatusChecker_Active(t *testing.T) {
	driver := &stubDriver{docs: map[string]db.Document{
		"tenant/acme": {"id": "acme", "status": "active"},
	}}
	checker := middleware.NewDbTenantStatusChecker(driver)
	ctx := auth.WithPlatformScope(context.Background())
	status, err := checker.TenantStatus(ctx, "acme")
	require.NoError(t, err)
	require.Equal(t, "active", status)
}

func TestDbTenantStatusChecker_Suspended(t *testing.T) {
	driver := &stubDriver{docs: map[string]db.Document{
		"tenant/acme": {"id": "acme", "status": "suspended"},
	}}
	checker := middleware.NewDbTenantStatusChecker(driver)
	ctx := auth.WithPlatformScope(context.Background())
	status, err := checker.TenantStatus(ctx, "acme")
	require.NoError(t, err)
	require.Equal(t, snoozetypes.TenantStatusSuspended, status)
}

func TestDbTenantStatusChecker_NotFound_ReturnsActive(t *testing.T) {
	driver := &stubDriver{docs: map[string]db.Document{}}
	checker := middleware.NewDbTenantStatusChecker(driver)
	ctx := auth.WithPlatformScope(context.Background())
	status, err := checker.TenantStatus(ctx, "missing")
	require.NoError(t, err)
	// Unknown tenant treated as active (fail-open on missing doc so a
	// misconfigured registry doesn't block all ingest).
	require.Equal(t, "active", status)
}
