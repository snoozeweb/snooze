package auth

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/snoozeweb/snooze/internal/condition"
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

func TestAPIKeyStore_RecordsLastUsed(t *testing.T) {
	d := newTestDriver(t)
	seedUser(t, d, "default", "alice", []string{"r1"}, true)
	// maxTTL must outlive the +2h Resolve #3 so the key is not expired before the
	// throttle window (APIKeyLastUsedInterval = 1h) reopens.
	s := NewAPIKeyStore(d, 30*24*time.Hour)
	ctx := snoozetypes.WithTenant(context.Background(), "default")

	// Synchronise on the fire-and-forget goroutine: afterTouch fires once the
	// throttled write has completed, so assertions never race the goroutine and
	// the goroutine never touches the driver after teardown.
	var wg sync.WaitGroup
	s.afterTouch = func() { wg.Done() }

	// Pin the clock so the throttle window is deterministic.
	base := time.Now().UTC()
	s.now = func() time.Time { return base }

	raw, _, err := s.Issue(ctx, ownerClaims(), "ci", []string{"ro_rule"}, time.Time{})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	readRow := func() db.Document {
		t.Helper()
		doc, err := d.GetOne(WithPlatformScope(ctx), APIKeyCollection, db.Document{"key_hash": hashToken(raw)})
		if err != nil || doc == nil {
			t.Fatalf("GetOne: doc=%v err=%v", doc, err)
		}
		return doc
	}

	// Resolve #1: fresh key → last_used_at populated, use_count == 1.
	wg.Add(1)
	if _, err := s.Resolve(ctx, raw); err != nil {
		t.Fatalf("Resolve #1: %v", err)
	}
	wg.Wait()
	doc := readRow()
	if asUnix(doc["last_used_at"]) == 0 {
		t.Fatalf("Resolve #1: last_used_at not set: %v", doc["last_used_at"])
	}
	if got, _ := doc["use_count"].(float64); got != 1 {
		t.Fatalf("Resolve #1: use_count = %v, want 1", doc["use_count"])
	}
	firstUsed := asUnix(doc["last_used_at"])

	// Resolve #2: within the throttle window → both fields unchanged, no write
	// (afterTouch must NOT fire, so we don't wg.Add here).
	s.now = func() time.Time { return base.Add(30 * time.Minute) }
	if _, err := s.Resolve(ctx, raw); err != nil {
		t.Fatalf("Resolve #2: %v", err)
	}
	doc = readRow()
	if got := asUnix(doc["last_used_at"]); got != firstUsed {
		t.Fatalf("Resolve #2: last_used_at changed %d -> %d", firstUsed, got)
	}
	if got, _ := doc["use_count"].(float64); got != 1 {
		t.Fatalf("Resolve #2: use_count = %v, want 1 (throttled)", doc["use_count"])
	}

	// Resolve #3: past the throttle window → last_used_at bumped, use_count == 2.
	s.now = func() time.Time { return base.Add(2 * time.Hour) }
	wg.Add(1)
	if _, err := s.Resolve(ctx, raw); err != nil {
		t.Fatalf("Resolve #3: %v", err)
	}
	wg.Wait()
	doc = readRow()
	if got := asUnix(doc["last_used_at"]); got <= firstUsed {
		t.Fatalf("Resolve #3: last_used_at not advanced: %d (was %d)", got, firstUsed)
	}
	if got, _ := doc["use_count"].(float64); got != 2 {
		t.Fatalf("Resolve #3: use_count = %v, want 2", doc["use_count"])
	}
}

// brokenWriteDriver delegates everything to a real driver except SetFields and
// IncMany, which always fail — so a failed last-used write cannot break auth.
type brokenWriteDriver struct {
	db.Driver
}

func (brokenWriteDriver) SetFields(context.Context, string, db.Document, condition.Cond) (int, error) {
	return 0, errors.New("boom")
}

func (brokenWriteDriver) IncMany(context.Context, string, string, condition.Cond, int64) (int, error) {
	return 0, errors.New("boom")
}

func TestAPIKeyStore_LastUsedFireAndForget(t *testing.T) {
	d := newTestDriver(t)
	seedUser(t, d, "default", "alice", []string{"r1"}, true)
	ctx := snoozetypes.WithTenant(context.Background(), "default")

	// Issue against the real driver so the key row exists for the read path.
	issuer := NewAPIKeyStore(d, time.Hour)
	raw, _, err := issuer.Issue(ctx, ownerClaims(), "ci", []string{"ro_rule"}, time.Time{})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	// Resolve through a store whose driver fails every last-used write.
	s := NewAPIKeyStore(brokenWriteDriver{Driver: d}, time.Hour)
	var wg sync.WaitGroup
	wg.Add(1)
	s.afterTouch = func() { wg.Done() }

	if _, err := s.Resolve(ctx, raw); err != nil {
		t.Fatalf("Resolve must succeed despite write failure, got: %v", err)
	}
	wg.Wait() // ensure the goroutine finished before teardown
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
