package core

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// fakeTenantDriver embeds db.Driver (nil) and overrides only Search, which is
// the single method housekeeper.ForEachTenant calls. Any other method would
// panic — none are exercised here.
type fakeTenantDriver struct {
	db.Driver
	tenants   []db.Document
	searchErr error // when set, Search fails (simulates a tenant-list failure)
}

func (f *fakeTenantDriver) Search(_ context.Context, coll string, _ condition.Cond, _ db.Page) ([]db.Document, int, error) {
	if f.searchErr != nil {
		return nil, 0, f.searchErr
	}
	if coll != "tenant" {
		return nil, 0, fmt.Errorf("unexpected collection %q", coll)
	}
	return f.tenants, len(f.tenants), nil
}

// fakeScanner records the tenant ctx of every ScanTenant call and can fail for
// a chosen tenant to prove one bad tenant does not abort the others.
type fakeScanner struct {
	mu     sync.Mutex
	seen   []string
	failOn string
}

func (s *fakeScanner) ScanTenant(ctx context.Context) error {
	tid, _ := snoozetypes.TenantFrom(ctx)
	s.mu.Lock()
	s.seen = append(s.seen, tid)
	s.mu.Unlock()
	if tid == s.failOn {
		return fmt.Errorf("scan failed for %s", tid)
	}
	return nil
}

func (s *fakeScanner) ScanInterval() time.Duration { return time.Hour }

func (s *fakeScanner) seenTenants() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := append([]string(nil), s.seen...)
	return out
}

func TestScanAllTenants_FansOutOverActiveTenants(t *testing.T) {
	drv := &fakeTenantDriver{tenants: []db.Document{
		{"id": "alpha", "status": "active"},
		{"id": "suspended-co", "status": "suspended"},
		{"id": "beta", "status": "active"},
	}}
	sc := &fakeScanner{}

	err := scanAllTenants(context.Background(), drv, sc, nil)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"alpha", "beta"}, sc.seenTenants(),
		"only active tenants are scanned; suspended is skipped")
}

func TestScanAllTenants_OneTenantErrorDoesNotAbortOthers(t *testing.T) {
	drv := &fakeTenantDriver{tenants: []db.Document{
		{"id": "alpha", "status": "active"},
		{"id": "beta", "status": "active"},
	}}
	sc := &fakeScanner{failOn: "alpha"}

	err := scanAllTenants(context.Background(), drv, sc, nil)
	require.NoError(t, err, "a per-tenant scan error must be swallowed")
	require.ElementsMatch(t, []string{"alpha", "beta"}, sc.seenTenants(),
		"beta must still be scanned after alpha fails")
}

func TestScanAllTenants_ListFailurePropagates(t *testing.T) {
	drv := &fakeTenantDriver{searchErr: fmt.Errorf("tenant list unavailable")}
	sc := &fakeScanner{}

	err := scanAllTenants(context.Background(), drv, sc, nil)
	require.Error(t, err, "a failure to list tenants must propagate, not be swallowed")
	require.Empty(t, sc.seenTenants(), "no tenant is scanned when the list itself fails")
}
