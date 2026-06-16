package auth

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/db/sqlite"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// newTestDriver returns a ready, on-disk SQLite driver. The plan asks to copy
// the harness from refresh_test.go, but that file uses the scope-ignoring
// fakeDB (which has no Delete and ignores conditions/duplicate policy). The
// APIKeyStore relies on real Delete + tenant scoping, so this mirrors the
// REAL driver harness from refresh_realdriver_test.go (sqlite.New) instead.
func newTestDriver(t *testing.T) db.Driver {
	t.Helper()
	path := filepath.Join(t.TempDir(), "apikey.db")
	drv, err := sqlite.New(context.Background(), sqlite.Config{Path: path})
	if err != nil {
		t.Fatalf("sqlite.New: %v", err)
	}
	t.Cleanup(func() { _ = drv.Close() })
	return drv
}

func seedUser(t *testing.T, d db.Driver, tenant, name string, roles []string, enabled bool) {
	t.Helper()
	ctx := snoozetypes.WithTenant(context.Background(), tenant)
	if _, err := d.Write(ctx, "role", []db.Document{{
		"name": "r1", "permissions": []any{"rw_record", "ro_rule"},
	}}, db.WriteOptions{Primary: []string{"tenant_id", "name"}, UpdateTime: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Write(ctx, LocalCollection, []db.Document{{
		"name": name, "method": LocalMethod, "enabled": enabled,
		"roles": toAnySlice(roles),
	}}, db.WriteOptions{Primary: []string{"tenant_id", "name", "method"}, UpdateTime: true}); err != nil {
		t.Fatal(err)
	}
}

func toAnySlice(in []string) []any {
	out := make([]any, len(in))
	for i, v := range in {
		out[i] = v
	}
	return out
}

func ownerClaims() snoozetypes.Claims {
	return snoozetypes.Claims{Subject: "alice", Method: LocalMethod, TenantID: "default", Permissions: []string{"rw_record", "ro_rule"}}
}

func TestAPIKeyStore_RoundTrip(t *testing.T) {
	d := newTestDriver(t)
	seedUser(t, d, "default", "alice", []string{"r1"}, true)
	s := NewAPIKeyStore(d, time.Hour)
	ctx := snoozetypes.WithTenant(context.Background(), "default")

	raw, doc, err := s.Issue(ctx, ownerClaims(), "ci", []string{"ro_rule"}, time.Time{})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if doc["key_hash"] != nil {
		t.Fatal("Issue must not return key_hash")
	}
	claims, err := s.Resolve(context.Background(), raw)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if claims.Subject != "alice" || claims.Method != APIKeyMethod {
		t.Fatalf("claims = %+v", claims)
	}
	if len(claims.Permissions) != 1 || claims.Permissions[0] != "ro_rule" {
		t.Fatalf("perms = %v", claims.Permissions)
	}
}

func TestAPIKeyStore_RejectsEscalation(t *testing.T) {
	d := newTestDriver(t)
	seedUser(t, d, "default", "alice", []string{"r1"}, true)
	s := NewAPIKeyStore(d, time.Hour)
	ctx := snoozetypes.WithTenant(context.Background(), "default")
	if _, _, err := s.Issue(ctx, ownerClaims(), "bad", []string{"rw_tenant"}, time.Time{}); err == nil {
		t.Fatal("expected escalation rejection")
	}
}

func TestAPIKeyStore_DisabledOwnerRejected(t *testing.T) {
	d := newTestDriver(t)
	seedUser(t, d, "default", "alice", []string{"r1"}, true)
	s := NewAPIKeyStore(d, time.Hour)
	ctx := snoozetypes.WithTenant(context.Background(), "default")
	raw, _, err := s.Issue(ctx, ownerClaims(), "ci", []string{"ro_rule"}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	seedUser(t, d, "default", "alice", []string{"r1"}, false) // disable
	if _, err := s.Resolve(context.Background(), raw); err == nil {
		t.Fatal("expected disabled-owner rejection")
	}
}

func TestAPIKeyStore_ExpiredRejected(t *testing.T) {
	d := newTestDriver(t)
	seedUser(t, d, "default", "alice", []string{"r1"}, true)
	s := NewAPIKeyStore(d, time.Hour)
	ctx := snoozetypes.WithTenant(context.Background(), "default")
	raw, _, err := s.Issue(ctx, ownerClaims(), "ci", []string{"ro_rule"}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	// Advance the clock past the cap so the key is expired at Resolve time.
	s.now = func() time.Time { return time.Now().Add(2 * time.Hour) }
	if _, err := s.Resolve(context.Background(), raw); err == nil {
		t.Fatal("expected expired-key rejection")
	}
}

func TestAPIKeyStore_DemotedOwnerShrinks(t *testing.T) {
	d := newTestDriver(t)
	seedUser(t, d, "default", "alice", []string{"r1"}, true)
	s := NewAPIKeyStore(d, time.Hour)
	ctx := snoozetypes.WithTenant(context.Background(), "default")

	// Mint a key carrying both perms the owner currently holds.
	raw, _, err := s.Issue(ctx, ownerClaims(), "ci", []string{"rw_record", "ro_rule"}, time.Time{})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	// Demote alice: drop the rw_record-bearing role by swapping role r1 to a
	// rule-only permission set.
	tctx := snoozetypes.WithTenant(context.Background(), "default")
	if _, err := d.Write(tctx, "role", []db.Document{{
		"name": "r1", "permissions": []any{"ro_rule"},
	}}, db.WriteOptions{Primary: []string{"tenant_id", "name"}, DuplicatePolicy: "replace", UpdateTime: true}); err != nil {
		t.Fatalf("demote: %v", err)
	}

	claims, err := s.Resolve(context.Background(), raw)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(claims.Permissions) != 1 || claims.Permissions[0] != "ro_rule" {
		t.Fatalf("expected perms to shrink to [ro_rule], got %v", claims.Permissions)
	}
}
