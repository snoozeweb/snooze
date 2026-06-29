package housekeeper

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/db"
)

// These tests reuse escalateFakeDriver (same package, escalate_timeout_test.go):
// an in-memory db.Driver that evaluates Search conditions with the real
// condition.Match and tracks records/comments/IncMany per tenant — exactly the
// surface UnshelveTimeoutJob exercises. fakeClock (housekeeper_test.go) supplies
// a deterministic deadline reference.

// TestUnshelveTimeoutJob_RevertsExpiredShelve: a record in state "shelved" whose
// shelve_until is in the past reverts to "open", shelve_until clears to 0, and
// an auto unshelve-comment is written (bumping comment_count).
func TestUnshelveTimeoutJob_RevertsExpiredShelve(t *testing.T) {
	clk := newFakeClock(time.Unix(1_000_000, 0))
	now := clk.Now().Unix()

	drv := newEscalateFakeDriver()
	drv.seedRecord("default", db.Document{
		"uid": "r-shelved", "state": "shelved", "shelve_until": now - 10,
	})

	ij := UnshelveTimeoutJob(drv, clk)
	require.Equal(t, time.Minute, ij.Interval)
	require.Equal(t, "unshelve_timeout", ij.Job.Name())
	runJob(t, ij)

	rec := drv.record("default", "r-shelved")
	require.Equal(t, "open", rec["state"], "expired shelve must revert to open")
	require.Equal(t, int64(0), rec["shelve_until"], "shelve_until must clear to 0")
	require.Equal(t, 1, drv.commentCount("default"), "expected one auto unshelve comment")
}

// TestUnshelveTimeoutJob_SkipsNonExpired: a shelved record whose shelve_until is
// still in the future is left untouched.
func TestUnshelveTimeoutJob_SkipsNonExpired(t *testing.T) {
	clk := newFakeClock(time.Unix(2_000_000, 0))
	now := clk.Now().Unix()

	drv := newEscalateFakeDriver()
	drv.seedRecord("default", db.Document{
		"uid": "r-future", "state": "shelved", "shelve_until": now + 3600,
	})

	runJob(t, UnshelveTimeoutJob(drv, clk))

	rec := drv.record("default", "r-future")
	require.Equal(t, "shelved", rec["state"], "a non-expired shelve must stay shelved")
	require.Equal(t, 0, drv.commentCount("default"), "no auto comment for a non-expired shelve")
}

// TestUnshelveTimeoutJob_SkipsZeroShelveUntil: the legacy permanent shelve
// (shelve_until==0) must NEVER be auto-unshelved — the `shelve_until > 0` guard
// excludes it.
func TestUnshelveTimeoutJob_SkipsZeroShelveUntil(t *testing.T) {
	clk := newFakeClock(time.Unix(3_000_000, 0))

	drv := newEscalateFakeDriver()
	drv.seedRecord("default", db.Document{
		"uid": "r-permanent", "state": "shelved", "shelve_until": int64(0),
	})

	runJob(t, UnshelveTimeoutJob(drv, clk))

	rec := drv.record("default", "r-permanent")
	require.Equal(t, "shelved", rec["state"], "permanent shelve (shelve_until==0) must never auto-unshelve")
	require.Equal(t, 0, drv.commentCount("default"))
}

// TestUnshelveTimeoutJob_PerTenant: the sweep runs inside each active tenant's
// scope; a suspended tenant is skipped.
func TestUnshelveTimeoutJob_PerTenant(t *testing.T) {
	clk := newFakeClock(time.Unix(4_000_000, 0))
	now := clk.Now().Unix()

	drv := newEscalateFakeDriver()
	drv.tenants = []db.Document{
		{"id": "default", "status": "active"},
		{"id": "acme", "status": "active"},
		{"id": "ghost", "status": "suspended"}, // must be skipped
	}
	drv.seedRecord("default", db.Document{"uid": "d-shelved", "state": "shelved", "shelve_until": now - 1})
	drv.seedRecord("acme", db.Document{"uid": "a-shelved", "state": "shelved", "shelve_until": now - 1})
	drv.seedRecord("ghost", db.Document{"uid": "g-shelved", "state": "shelved", "shelve_until": now - 1})

	runJob(t, UnshelveTimeoutJob(drv, clk))

	require.Equal(t, "open", drv.record("default", "d-shelved")["state"])
	require.Equal(t, "open", drv.record("acme", "a-shelved")["state"])
	// Suspended tenant skipped: its record is never touched.
	require.Equal(t, "shelved", drv.record("ghost", "g-shelved")["state"])
}
