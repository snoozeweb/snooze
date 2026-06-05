// internal/housekeeper/tenant_test.go
package housekeeper

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/db"
)

// tenantStubDriver is a minimal db.Driver stub that returns a fixed list of
// tenant documents from Search and records Write calls.
type tenantStubDriver struct {
	db.Driver // embed zero-value to satisfy the interface; override only needed methods
	tenants   []db.Document
}

func (s *tenantStubDriver) Search(_ context.Context, collection string, _ condition.Cond, _ db.Page) ([]db.Document, int, error) {
	if collection == "tenant" {
		return s.tenants, len(s.tenants), nil
	}
	return nil, 0, nil
}

func TestForEachTenant_CallsPerTenant(t *testing.T) {
	drv := &tenantStubDriver{tenants: []db.Document{
		{"id": "acme", "status": "active"},
		{"id": "beta", "status": "active"},
	}}

	var mu sync.Mutex
	visited := []string{}

	err := ForEachTenant(context.Background(), drv, func(ctx context.Context, tenantID string) error {
		got, ok := auth.TenantFrom(ctx)
		require.True(t, ok)
		require.Equal(t, tenantID, got)
		mu.Lock()
		visited = append(visited, tenantID)
		mu.Unlock()
		return nil
	})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"acme", "beta"}, visited)
}

func TestForEachTenant_SkipsSuspended(t *testing.T) {
	drv := &tenantStubDriver{tenants: []db.Document{
		{"id": "acme", "status": "active"},
		{"id": "frozen", "status": "suspended"},
	}}

	var visited []string
	err := ForEachTenant(context.Background(), drv, func(_ context.Context, tenantID string) error {
		visited = append(visited, tenantID)
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, []string{"acme"}, visited)
}

func TestForEachTenant_PropagatesError(t *testing.T) {
	drv := &tenantStubDriver{tenants: []db.Document{
		{"id": "acme", "status": "active"},
	}}
	boom := errors.New("boom")
	err := ForEachTenant(context.Background(), drv, func(_ context.Context, _ string) error {
		return boom
	})
	require.ErrorIs(t, err, boom)
}
