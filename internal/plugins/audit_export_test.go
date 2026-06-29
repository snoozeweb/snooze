package plugins

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// TestEmitBulkAudit_WritesOneRowPerUID pins the exported EmitBulkAudit helper:
// it must write exactly one audit document per affected uid, with the canonical
// audit schema (object_type/object_id/action/username/method/summary/date_epoch),
// and it must no-op when the plugin's metadata opts out of auditing.
func TestEmitBulkAudit_WritesOneRowPerUID(t *testing.T) {
	t.Parallel()
	memo := newMemDB()
	h := newNullHost(memo)

	ctx := auth.WithClaims(context.Background(), snoozetypes.Claims{
		Subject: "alice",
		Method:  "local",
	})

	uids := []string{"r1", "r2", "r3"}
	EmitBulkAudit(ctx, h, Metadata{Audit: true}, "record", "bulk_state", uids, "ack: maint window")

	rows := auditDocs(memo)
	require.Len(t, rows, len(uids))
	seen := map[string]bool{}
	for _, row := range rows {
		require.Equal(t, "record", row["object_type"])
		require.Equal(t, "bulk_state", row["action"])
		require.Equal(t, "alice", row["username"])
		require.Equal(t, "local", row["method"])
		require.Equal(t, "ack: maint window", row["summary"])
		require.IsType(t, float64(0), row["date_epoch"])
		seen[row["object_id"].(string)] = true
	}
	require.Equal(t, map[string]bool{"r1": true, "r2": true, "r3": true}, seen)
}

// TestEmitBulkAudit_NoopWhenAuditDisabled verifies the helper writes nothing
// when the plugin's metadata.Audit is false (the default for noisy collections
// such as record/comment), mirroring the private emitAudit short-circuit.
func TestEmitBulkAudit_NoopWhenAuditDisabled(t *testing.T) {
	t.Parallel()
	memo := newMemDB()
	h := newNullHost(memo)

	EmitBulkAudit(context.Background(), h, Metadata{Audit: false}, "record", "bulk_state",
		[]string{"r1", "r2"}, "ack")

	require.Empty(t, auditDocs(memo))
}

// TestEmitBulkAudit_NoopOnAuditCollection guards against the audit-of-audit
// recursion: a write to the audit collection itself never emits a row.
func TestEmitBulkAudit_NoopOnAuditCollection(t *testing.T) {
	t.Parallel()
	memo := newMemDB()
	h := newNullHost(memo)

	EmitBulkAudit(context.Background(), h, Metadata{Audit: true}, auditCollection, "bulk_state",
		[]string{"a1"}, "noop")

	require.Empty(t, auditDocs(memo))
}
