package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/ownership"
	"github.com/snoozeweb/snooze/internal/resolutionhold"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

func ownerCtx() context.Context {
	return snoozetypes.WithTenant(context.Background(), snoozetypes.DefaultTenant)
}

// seedOwnerRecords writes records and returns their uids in order.
func seedOwnerRecords(t *testing.T, d db.Driver, docs ...db.Document) []string {
	t.Helper()
	res, err := d.Write(ownerCtx(), "record", docs, db.WriteOptions{})
	require.NoError(t, err)
	require.Len(t, res.Added, len(docs))
	return res.Added
}

func getRecord(t *testing.T, d db.Driver, uid string) db.Document {
	t.Helper()
	doc, err := d.GetOne(ownerCtx(), "record", db.Document{"uid": uid})
	require.NoError(t, err)
	return doc
}

// owned is a record document already owned by who (method ldap).
func owned(who string, extra db.Document) db.Document {
	doc := db.Document{}
	for k, v := range ownership.Take(who, "ldap", 50) {
		doc[k] = v
	}
	for k, v := range extra {
		doc[k] = v
	}
	return doc
}

// A bulk ack/close makes the caller the owner of every match, exactly like
// the single-record comment path.
func TestBulkState_AckTakesOwnership(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"ack", "close"} {
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			r, d := bulkHarness(t, false)
			uids := seedOwnerRecords(t, d,
				owned("alice", db.Document{"host": "h1", "previous_owner": "zed"}),
				db.Document{"host": "h1"},
				db.Document{"host": "h2"},
			)

			q := encodeQ(t, condition.Equals("host", "h1"))
			rec := bulkReq(t, r, "/api/v1/record/bulk_state?q="+q, map[string]any{"state": state}, "rw_record")
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

			for _, uid := range uids[:2] {
				doc := getRecord(t, d, uid)
				require.Equal(t, state, doc["state"])
				require.Equal(t, "tester", doc["owner"], "authReq's subject")
				require.Equal(t, "local", doc["owner_method"])
				require.NotZero(t, doc["owner_since"])
				require.Equal(t, "", doc["previous_owner"])
			}
			_, has := getRecord(t, d, uids[2])["owner"]
			require.False(t, has, "a non-match is untouched")
		})
	}
}

// A bulk ack made with an API key stamps the key owner's login method.
func TestBulkState_APIKeyAckUsesOwnerMethod(t *testing.T) {
	t.Parallel()
	r, d := bulkHarness(t, false)
	uids := seedOwnerRecords(t, d, db.Document{"host": "h1"})

	q := encodeQ(t, condition.Equals("host", "h1"))
	body, err := json.Marshal(map[string]any{"state": "ack"})
	require.NoError(t, err)
	req := authReq("POST", "/api/v1/record/bulk_state?q="+q, body, "rw_record")
	req = req.WithContext(auth.WithClaims(req.Context(), snoozetypes.Claims{
		Subject: "tester", Method: auth.APIKeyMethod, OwnerMethod: "ldap",
		Permissions: []string{"rw_record"},
	}))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	doc := getRecord(t, d, uids[0])
	require.Equal(t, "tester", doc["owner"])
	require.Equal(t, "ldap", doc["owner_method"])
}

// A bulk open/esc clears ownership PER ROW: each record's own owner becomes
// its ghost, an unowned record keeps whatever ghost it had. The query refers
// to the pre-transition state, so the clear must run before the state flip.
func TestBulkState_EscClearsOwnershipPerRow(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"open", "esc"} {
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			r, d := bulkHarness(t, false)
			uids := seedOwnerRecords(t, d,
				owned("alice", db.Document{"state": "ack"}),
				owned("bob", db.Document{"state": "ack"}),
				db.Document{"state": "ack", "owner": "", "previous_owner": "carol"},
				owned("dave", db.Document{"state": "close"}),
			)

			q := encodeQ(t, condition.Equals("state", "ack"))
			rec := bulkReq(t, r, "/api/v1/record/bulk_state?q="+q, map[string]any{"state": state}, "rw_record")
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

			var body bulkStateResponse
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			require.Equal(t, 3, body.Matched)

			for i, prev := range []string{"alice", "bob", "carol"} {
				doc := getRecord(t, d, uids[i])
				require.Equal(t, state, doc["state"])
				require.Equal(t, "", doc["owner"])
				require.Equal(t, prev, doc["previous_owner"])
			}
			require.Equal(t, "ldap", getRecord(t, d, uids[0])["previous_owner_method"])
			require.Equal(t, "dave", getRecord(t, d, uids[3])["owner"], "a non-match keeps its owner")
		})
	}
}

// seedUsers writes the directory the assignee validation reads.
func seedUsers(t *testing.T, d db.Driver) {
	t.Helper()
	_, err := d.Write(ownerCtx(), "user", []db.Document{
		{"name": "bob", "method": "local", "enabled": true},
		{"name": "carol", "method": "local", "enabled": false},
		{"name": "dave", "method": "local", "enabled": true},
		{"name": "dave", "method": "ldap", "enabled": true},
	}, db.WriteOptions{})
	require.NoError(t, err)
}

func TestBulkOwner_AssignSkipsClosed(t *testing.T) {
	t.Parallel()
	r, d := bulkHarness(t, true)
	seedUsers(t, d)
	uids := seedOwnerRecords(t, d,
		owned("alice", db.Document{"host": "h1", "state": "ack", "acked_by": "alice"}),
		db.Document{"host": "h1", "state": "open"},
		owned("alice", db.Document{"host": "h1", "state": "close"}),
		db.Document{"host": "h2", "state": "open"},
	)

	q := encodeQ(t, condition.Equals("host", "h1"))
	rec := bulkReq(t, r, "/api/v1/record/bulk_owner?q="+q,
		map[string]any{"action": "assign", "assignee": "bob", "message": "handover"}, "rw_record")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var body bulkOwnerResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, bulkOwnerResponse{Matched: 3, Updated: 2, Action: "assign"}, body)

	for _, uid := range uids[:2] {
		doc := getRecord(t, d, uid)
		require.Equal(t, "bob", doc["owner"])
		require.Equal(t, "local", doc["owner_method"], "filled in from the unique user")
		require.Equal(t, "", doc["previous_owner"])
	}
	first := getRecord(t, d, uids[0])
	require.Equal(t, "ack", first["state"], "assign is not a state change")
	require.Equal(t, "alice", first["acked_by"], "acked_by is unchanged (D7)")
	require.Equal(t, "alice", getRecord(t, d, uids[2])["owner"], "closed records are skipped")
	_, has := getRecord(t, d, uids[3])["owner"]
	require.False(t, has)

	audits, _, err := d.Search(ownerCtx(), "audit", noCond(), db.Page{})
	require.NoError(t, err)
	require.Len(t, audits, 2, "one audit row per assigned record")
	for _, a := range audits {
		require.Equal(t, "bulk_owner", a["action"])
		require.Equal(t, "assign bob: handover", a["summary"])
	}
}

func TestBulkOwner_AssignExplicitMethod(t *testing.T) {
	t.Parallel()
	r, d := bulkHarness(t, false)
	seedUsers(t, d)
	uids := seedOwnerRecords(t, d, db.Document{"state": "open"})

	rec := bulkReq(t, r, "/api/v1/record/bulk_owner",
		map[string]any{"action": "assign", "assignee": "dave", "assignee_method": "ldap"}, "rw_record")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "ldap", getRecord(t, d, uids[0])["owner_method"])
}

func TestBulkOwner_RejectsBadRequests(t *testing.T) {
	t.Parallel()
	r, d := bulkHarness(t, false)
	seedUsers(t, d)
	uids := seedOwnerRecords(t, d, db.Document{"state": "open"})

	for name, tc := range map[string]struct {
		body map[string]any
		code int
	}{
		"unknown action":   {map[string]any{"action": "steal"}, http.StatusBadRequest},
		"missing assignee": {map[string]any{"action": "assign"}, http.StatusBadRequest},
		"unknown user":     {map[string]any{"action": "assign", "assignee": "nobody"}, http.StatusUnprocessableEntity},
		"disabled user":    {map[string]any{"action": "assign", "assignee": "carol"}, http.StatusUnprocessableEntity},
		"ambiguous login":  {map[string]any{"action": "assign", "assignee": "dave"}, http.StatusUnprocessableEntity},
		"wrong method":     {map[string]any{"action": "assign", "assignee": "bob", "assignee_method": "oidc"}, http.StatusUnprocessableEntity},
	} {
		t.Run(name, func(t *testing.T) {
			rec := bulkReq(t, r, "/api/v1/record/bulk_owner", tc.body, "rw_record")
			require.Equal(t, tc.code, rec.Code, rec.Body.String())
		})
	}
	_, has := getRecord(t, d, uids[0])["owner"]
	require.False(t, has, "a rejected request writes nothing")
}

func TestBulkOwner_RequiresRwRecord(t *testing.T) {
	t.Parallel()
	r, _ := bulkHarness(t, false)
	rec := bulkReq(t, r, "/api/v1/record/bulk_owner", map[string]any{"action": "release"}, "ro_record")
	require.Equal(t, http.StatusForbidden, rec.Code)
	rec = bulkReq(t, r, "/api/v1/record/bulk_owner", map[string]any{"action": "release"}, "rw_all")
	require.Equal(t, http.StatusOK, rec.Code)
}

// A bulk release mirrors the single-record one per row: the owner becomes the
// ghost, an acknowledged record goes back to open (ack expiry lifted,
// acked_by removed), any other state is left as it is, and unowned matches are
// not touched at all.
func TestBulkOwner_Release(t *testing.T) {
	t.Parallel()
	r, d := bulkHarness(t, true)
	uids := seedOwnerRecords(t, d,
		owned("alice", db.Document{"host": "h1", "state": "ack", "acked_by": "alice", "ack_until": int64(999)}),
		owned("bob", db.Document{"host": "h1", "state": "esc", "acked_by": "bob"}),
		db.Document{"host": "h1", "state": "ack", "acked_by": "zed", "previous_owner": "carol"},
	)

	q := encodeQ(t, condition.Equals("host", "h1"))
	rec := bulkReq(t, r, "/api/v1/record/bulk_owner?q="+q,
		map[string]any{"action": "release"}, "rw_record")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var body bulkOwnerResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, bulkOwnerResponse{Matched: 3, Updated: 2, Action: "release"}, body)

	acked := getRecord(t, d, uids[0])
	require.Equal(t, "open", acked["state"])
	require.EqualValues(t, 0, acked["ack_until"])
	require.Equal(t, "", acked["owner"])
	require.Equal(t, "alice", acked["previous_owner"])
	require.Equal(t, "ldap", acked["previous_owner_method"])
	_, has := acked["acked_by"]
	require.False(t, has, "releasing an ack removes acked_by like a manual open")

	esc := getRecord(t, d, uids[1])
	require.Equal(t, "esc", esc["state"])
	require.Equal(t, "bob", esc["acked_by"])
	require.Equal(t, "", esc["owner"])
	require.Equal(t, "bob", esc["previous_owner"])

	unowned := getRecord(t, d, uids[2])
	require.Equal(t, "ack", unowned["state"], "an unowned match is not released")
	require.Equal(t, "zed", unowned["acked_by"])
	require.Equal(t, "carol", unowned["previous_owner"])

	audits, _, err := d.Search(ownerCtx(), "audit", noCond(), db.Page{})
	require.NoError(t, err)
	require.Len(t, audits, 2, "one audit row per released record")
	got := map[any]bool{}
	for _, a := range audits {
		require.Equal(t, "bulk_owner", a["action"])
		require.Equal(t, "release", a["summary"])
		got[a["object_id"]] = true
	}
	require.True(t, got[uids[0]] && got[uids[1]])
}

// A bulk close by a human arms the resolution hold like the single-record
// close; a bulk open ends it.
func TestBulkState_ResolutionHold(t *testing.T) {
	t.Parallel()
	r, d := bulkHarness(t, false)
	uids := seedOwnerRecords(t, d, db.Document{"host": "h1", "state": "ack"})
	q := encodeQ(t, condition.Equals("host", "h1"))

	rec := bulkReq(t, r, "/api/v1/record/bulk_state?q="+q, map[string]any{"state": "close"}, "rw_record")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	doc := getRecord(t, d, uids[0])
	until, _ := doc[resolutionhold.FieldUntil].(int64)
	if f, ok := doc[resolutionhold.FieldUntil].(float64); ok {
		until = int64(f)
	}
	require.Greater(t, until, time.Now().Unix(), "human bulk close arms a hold in the future")

	rec = bulkReq(t, r, "/api/v1/record/bulk_state?q="+q, map[string]any{"state": "open"}, "rw_record")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.EqualValues(t, 0, getRecord(t, d, uids[0])[resolutionhold.FieldUntil])
}

func TestSourceTag(t *testing.T) {
	require.Equal(t, "", sourceTag("  "))
	require.Equal(t, " [snooze-skill]", sourceTag("snooze-skill"))
	require.Len(t, []rune(sourceTag(strings.Repeat("é", 100))), 64+3)
}
