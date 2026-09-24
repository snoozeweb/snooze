package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/db/sqlite"
	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"

	// The real plugins, so the routes are gated by their shipped policies.
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/comment"
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/notificationlog"
)

// runsHarness mounts the runs routes beside the two plugins' generic CRUD, in
// router.go's order, over a fresh SQLite store.
func runsHarness(t *testing.T) (chi.Router, db.Driver) {
	t.Helper()
	ctx := snoozetypes.WithTenant(context.Background(), snoozetypes.DefaultTenant)
	d, err := sqlite.New(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "snooze.db")})
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })

	plugs := map[string]plugins.Plugin{}
	for _, name := range []string{plugins.NotificationLogCollection, commentCollection} {
		p, err := plugins.New(name)
		require.NoError(t, err)
		plugs[name] = p
	}
	host := &bulkTestHost{driver: d, plugs: plugs}
	rt := &Router{DB: d, Host: host, Plugins: plugs}
	r := chi.NewRouter()
	rt.mountRuns(r)
	for _, p := range plugs {
		plugins.MountCRUD(r, host, p)
	}
	return r, d
}

func seed(t *testing.T, d db.Driver, collection string, docs ...db.Document) {
	t.Helper()
	ctx := snoozetypes.WithTenant(context.Background(), snoozetypes.DefaultTenant)
	_, err := d.Write(ctx, collection, docs, db.WriteOptions{})
	require.NoError(t, err)
}

// send is one delivery-log row of alert "a1" routed by notification "n1".
func send(queued int64, action, status string) db.Document {
	return db.Document{
		"action":             action,
		"status":             status,
		"queued_epoch":       queued,
		"date_epoch":         queued,
		"alert_uids":         []any{"a1"},
		"alert_hashes":       []any{"h1"},
		"alert_count":        1,
		"notification_uids":  []any{"n1"},
		"notification_names": []any{"Kube Prod"},
		"batch":              false,
		"escalation_count":   0,
	}
}

func getJSON[T any](t *testing.T, r chi.Router, target string, perms ...string) (*httptest.ResponseRecorder, T) {
	t.Helper()
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, authReq(http.MethodGet, target, nil, perms...))
	var out T
	if rec.Code == http.StatusOK {
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	}
	return rec, out
}

func TestDeliveryRuns_FoldsConsecutiveRepeats(t *testing.T) {
	t.Parallel()
	r, d := runsHarness(t)
	var rows []db.Document
	// Oldest first: three dispatches to {Jira Ticket, Teams}, then one where
	// Teams failed, then four to {Jira Escalation, Teams}, 960s apart.
	at := int64(1_000_000)
	for i := 0; i < 3; i++ {
		rows = append(rows, send(at, "Jira Ticket", "success"), send(at, "Teams", "success"))
		at += 960
	}
	rows = append(rows, send(at, "Jira Ticket", "success"), send(at, "Teams", "error"))
	at += 960
	for i := 0; i < 4; i++ {
		rows = append(rows, send(at, "Jira Escalation", "success"), send(at, "Teams", "success"))
		at += 960
	}
	// Another alert's send never shows up.
	other := send(at, "Teams", "success")
	other["alert_uids"] = []any{"a2"}
	rows = append(rows, other)
	seed(t, d, plugins.NotificationLogCollection, rows...)

	rec, out := getJSON[deliveryRunsResponse](t, r, "/api/v1/notificationlog/runs?alert_uid=a1", "ro_notificationlog")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	require.Len(t, out.Data, 3, "escalation run, the failure alone, the ticket run")
	esc, failed, ticket := out.Data[0], out.Data[1], out.Data[2]
	require.Equal(t, 4, esc.Dispatches)
	require.Equal(t, 8, esc.Sends)
	require.Equal(t, int64(960), esc.IntervalS)
	require.Equal(t, int64(1_000_000+4*960), esc.FirstEpoch)
	require.Equal(t, int64(1_000_000+7*960), esc.LastEpoch)
	require.Len(t, esc.Latest, 2, "the newest dispatch's rows")
	require.Equal(t, 1, failed.Dispatches, "a failure never joins a run")
	require.Equal(t, 3, ticket.Dispatches)
	require.Equal(t, int64(1_000_000), ticket.FirstEpoch)
	require.False(t, ticket.Truncated)

	require.Equal(t, deliveryRunsMeta{
		Total: 3, Sends: 16, Dispatches: 8,
		FirstEpoch: 1_000_000, LastEpoch: 1_000_000 + 7*960, IntervalS: 960,
	}, out.Meta)

	// Paging walks the runs, not the rows.
	_, page := getJSON[deliveryRunsResponse](t, r, "/api/v1/notificationlog/runs?alert_uid=a1&limit=1&offset=2", "ro_notificationlog")
	require.Len(t, page.Data, 1)
	require.Equal(t, ticket.Key, page.Data[0].Key)
	require.Equal(t, 3, page.Meta.Total)
}

func TestDeliveryRuns_InterleavedNotificationsFoldSeparately(t *testing.T) {
	t.Parallel()
	r, d := runsHarness(t)
	var rows []db.Document
	for i := int64(0); i < 3; i++ {
		a := send(1000+i*900, "Teams", "success")
		b := send(1000+i*900+60, "Mail", "success")
		b["notification_uids"] = []any{"n2"}
		b["notification_names"] = []any{"Mail on-call"}
		rows = append(rows, a, b)
	}
	seed(t, d, plugins.NotificationLogCollection, rows...)
	_, out := getJSON[deliveryRunsResponse](t, r, "/api/v1/notificationlog/runs?alert_uid=a1", "ro_notificationlog")
	require.Len(t, out.Data, 2, "one run per notification, not six runs of one")
	require.Equal(t, "Mail", out.Data[0].Latest[0]["action"], "newest run first")
	require.Equal(t, 3, out.Data[0].Dispatches)
	require.Equal(t, 3, out.Data[1].Dispatches)
	require.Equal(t, int64(900), out.Data[1].IntervalS)
}

func TestDeliveryRuns_NewTicketReferenceStandsAlone(t *testing.T) {
	t.Parallel()
	r, d := runsHarness(t)
	var rows []db.Document
	// Oldest first, five dispatches of {Jira, Teams}, 960s apart. Jira quotes
	// CG-1 on every send (first seen on the oldest), opens AD-775 once, on the
	// third; Teams' thread ref is plumbing and never counts.
	for i := int64(0); i < 5; i++ {
		at := 1000 + i*960
		jira := send(at, "Jira", "success")
		jira["ref"] = map[string]any{"issue_key": "CG-1"}
		if i == 2 {
			jira["ref"] = map[string]any{"issue_key": "AD-775", "url": "https://jira/browse/AD-775"}
		}
		teams := send(at, "Teams", "success")
		teams["ref"] = map[string]any{"message_ids": map[string]any{"t": "1"}}
		rows = append(rows, jira, teams)
	}
	seed(t, d, plugins.NotificationLogCollection, rows...)
	_, out := getJSON[deliveryRunsResponse](t, r, "/api/v1/notificationlog/runs?alert_uid=a1", "ro_notificationlog")
	counts := make([]int, len(out.Data))
	for i, run := range out.Data {
		counts[i] = run.Dispatches
	}
	// Newest first: two plain repeats, AD-775 alone, one repeat, CG-1's first.
	require.Equal(t, []int{2, 1, 1, 1}, counts)
	require.Equal(t, int64(1000+2*960), out.Data[1].LastEpoch)
	require.Equal(t, int64(1000), out.Data[3].FirstEpoch)
}

func TestDeliveryRuns_KeyStableWhileTheRunGrows(t *testing.T) {
	t.Parallel()
	r, d := runsHarness(t)
	seed(t, d, plugins.NotificationLogCollection, send(100, "Teams", "success"), send(200, "Teams", "success"))
	_, before := getJSON[deliveryRunsResponse](t, r, "/api/v1/notificationlog/runs?alert_uid=a1", "ro_notificationlog")
	seed(t, d, plugins.NotificationLogCollection, send(300, "Teams", "success"))
	_, after := getJSON[deliveryRunsResponse](t, r, "/api/v1/notificationlog/runs?alert_uid=a1", "ro_notificationlog")
	require.Len(t, after.Data, 1)
	require.Equal(t, 3, after.Data[0].Dispatches)
	require.Equal(t, before.Data[0].Key, after.Data[0].Key, "keyed by the oldest dispatch")
}

func TestDeliveryRuns_EmptyAndErrors(t *testing.T) {
	t.Parallel()
	r, _ := runsHarness(t)
	rec, _ := getJSON[deliveryRunsResponse](t, r, "/api/v1/notificationlog/runs?alert_uid=none", "ro_notificationlog")
	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{"data":[],"meta":{"total":0,"sends":0,"dispatches":0,"first_epoch":0,"last_epoch":0,"interval_s":0,"truncated":false}}`, rec.Body.String())

	rec, _ = getJSON[deliveryRunsResponse](t, r, "/api/v1/notificationlog/runs", "ro_notificationlog")
	require.Equal(t, http.StatusBadRequest, rec.Code, "an alert is required: the whole log is not offered")
	rec, _ = getJSON[deliveryRunsResponse](t, r, "/api/v1/notificationlog/runs?alert_uid=a1&limit=0", "ro_notificationlog")
	require.Equal(t, http.StatusBadRequest, rec.Code)

	rec, _ = getJSON[deliveryRunsResponse](t, r, "/api/v1/notificationlog/runs?alert_uid=a1", "ro_notification")
	require.Equal(t, http.StatusForbidden, rec.Code, "gated like the log itself")
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/notificationlog/runs?alert_uid=a1", nil))
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func comment(epoch int64, ctype, message string, auto bool) db.Document {
	doc := db.Document{"record_uid": "r1", "type": ctype, "message": message, "date_epoch": epoch}
	if auto {
		doc["auto"] = true
	} else {
		doc["user"] = "alice"
		doc["method"] = "local"
	}
	return doc
}

func TestCommentRuns_FoldsAutoRepeatsOnly(t *testing.T) {
	t.Parallel()
	r, d := runsHarness(t)
	seed(t, d, commentCollection,
		comment(100, "comment", "New escalation", true),
		comment(200, "comment", "New escalation", true),
		comment(300, "comment", "Looking", false),
		comment(400, "comment", "New escalation", true),
		comment(500, "comment", "New escalation", true),
		comment(700, "comment", "New escalation", true),
		comment(800, "esc", "Escalation timeout", true),
		// A person repeating themselves is still two entries.
		comment(900, "comment", "Ping", false),
		comment(950, "comment", "Ping", false),
	)
	// Another record's comment never shows up.
	seed(t, d, commentCollection, db.Document{"record_uid": "r2", "type": "comment", "message": "x", "date_epoch": int64(1000), "auto": true})

	rec, out := getJSON[commentRunsResponse](t, r, "/api/v1/comment/runs?record_uid=r1&limit=20")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	counts := make([]int, len(out.Data))
	for i, run := range out.Data {
		counts[i] = run.Count
	}
	require.Equal(t, []int{1, 1, 1, 3, 1, 2}, counts)
	run := out.Data[3]
	require.Equal(t, int64(400), run.FirstEpoch)
	require.Equal(t, int64(700), run.LastEpoch)
	require.Equal(t, int64(200), run.IntervalS, "median of gaps 200 and 100: the upper middle")
	require.Equal(t, "New escalation", run.Latest["message"])
	require.Equal(t, commentRunsMeta{Total: 6, Comments: 9}, out.Meta)

	rec, _ = getJSON[commentRunsResponse](t, r, "/api/v1/comment/runs")
	require.Equal(t, http.StatusBadRequest, rec.Code)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/comment/runs?record_uid=r1", nil))
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestMedianGap(t *testing.T) {
	t.Parallel()
	require.Equal(t, int64(0), medianGap(nil))
	require.Equal(t, int64(0), medianGap([]int64{5}))
	// One week-long pause inside a quarter-hourly run does not move it.
	require.Equal(t, int64(900), medianGap([]int64{604800 + 2700, 2700, 1800, 900, 0}))
}
