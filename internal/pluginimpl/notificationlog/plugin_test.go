package notificationlog

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/config"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/db/sqlite"
	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/internal/telemetry"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

const collection = plugins.NotificationLogCollection

type testHost struct {
	drv *sqlite.Driver
	cfg *config.Config
}

func newTestHost(t *testing.T) *testHost {
	t.Helper()
	path := filepath.Join(t.TempDir(), "snooze.db")
	drv, err := sqlite.New(context.Background(), sqlite.Config{Path: path})
	require.NoError(t, err)
	t.Cleanup(func() { _ = drv.Close() })
	return &testHost{drv: drv, cfg: config.Default()}
}

func (h *testHost) DB() db.Driver                { return h.drv }
func (h *testHost) Bus() plugins.Bus             { return nil }
func (h *testHost) Logger() *slog.Logger         { return slog.Default() }
func (h *testHost) Tracer() trace.Tracer         { return otel.Tracer("notificationlog-test") }
func (h *testHost) Metrics() *telemetry.Registry { return telemetry.NewRegistry(nil) }
func (h *testHost) Config() *config.Config       { return h.cfg }
func (h *testHost) Plugin(string) plugins.Plugin { return nil }

func tenantCtx(tenant string) context.Context {
	return auth.WithTenant(context.Background(), tenant)
}

func sampleRow(action, notifUID, alertUID string) plugins.DeliveryRow {
	return plugins.DeliveryRow{
		CompletedAt: time.Unix(1757340000, 0),
		Duration:    412 * time.Millisecond,
		Status:      plugins.DeliveryStatusSuccess,
		Action:      action,
		Notifier:    "mail",
		Members: []plugins.DeliveryMember{{
			UID:             alertUID,
			Hash:            "h-" + alertUID,
			Host:            "db-01",
			Severity:        "critical",
			Message:         "disk 98%",
			State:           "open",
			Notification:    "page-oncall",
			NotificationUID: notifUID,
			QueuedAt:        time.Unix(1757339998, 0),
		}},
	}
}

func TestRegistration(t *testing.T) {
	require.True(t, slices.Contains(plugins.Registered(), collection))
	require.Equal(t, "notificationlog", collection)
}

// TestMetadata pins the settings that make this collection behave: writes do
// NOT emit audit rows (the dispatcher writes one row per send — auditing them
// would double the volume) and the scalar dimensions are registered as
// search_fields so the drivers index them.
//
// There is deliberately no assertion on route_defaults.check_permissions: the
// key parses but nothing reads it (authz.go derives ro_/rw_<plugin> from the
// plugin name), so asserting it would fake security coverage. The real gate is
// AuthorizeCRUD, exercised by TestHTTPWritesForbidden below.
func TestMetadata(t *testing.T) {
	meta, err := plugins.ParseMetadata(metaYAML)
	require.NoError(t, err)
	require.Equal(t, "Notification log", meta.Name)
	require.Equal(t, "Per-send delivery history written by the notification dispatcher", meta.DisplayName)
	require.False(t, meta.Audit, "delivery rows must not be audited")
	require.Equal(t, []string{"date_epoch", "status", "action", "notifier"}, meta.SearchFields,
		"the scalar dimensions must be registered so SQLite/Postgres index them")

	p := &Plugin{meta: meta}
	require.Equal(t, collection, p.Name())
	require.Equal(t, meta, p.Metadata())
}

// TestNotGlobalCollection guards the tenancy contract: delivery rows describe
// tenant-scoped alerts, so the driver must inject the tenant predicate.
func TestNotGlobalCollection(t *testing.T) {
	require.False(t, db.IsGlobalCollection(collection))
}

func TestSchema(t *testing.T) {
	p := &Plugin{}
	schema, ok := p.Schema().(map[string]any)
	require.True(t, ok)
	props, ok := schema["properties"].(map[string]any)
	require.True(t, ok)

	for _, f := range []string{
		"date_epoch", "queued_epoch", "duration_ms", "status", "error",
		"action", "notifier", "batch", "batch_reason",
		"notification_uids", "notification_names",
		"alert_count", "alert_uids", "alert_hashes", "alerts",
		"escalation_count", "escalation_reason", "ref",
	} {
		require.Contains(t, props, f)
	}

	status, ok := props["status"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, []any{"success", "error"}, status["enum"])

	alerts, ok := props["alerts"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "array", alerts["type"])
	item, ok := alerts["items"].(map[string]any)
	require.True(t, ok)
	itemProps, ok := item["properties"].(map[string]any)
	require.True(t, ok)
	require.Contains(t, itemProps, "severity")
	require.Contains(t, itemProps, "notification")

	uids, ok := props["alert_uids"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "array", uids["type"])

	// Counts and durations are whole numbers. The types here must match
	// api/openapi.yaml's NotificationLogEntry, or the generated TS client and
	// the server disagree about the row shape.
	for _, f := range []string{"duration_ms", "alert_count", "escalation_count"} {
		spec, ok := props[f].(map[string]any)
		require.True(t, ok, f)
		require.Equal(t, "integer", spec["type"], "%s must match the OpenAPI schema", f)
	}
}

func TestValidateAcceptsAnyMap(t *testing.T) {
	p := &Plugin{}
	require.NoError(t, p.Validate(nil))
	require.NoError(t, p.Validate(map[string]any{}))
	require.NoError(t, p.Validate(map[string]any{"status": "error", "alert_count": 3}))
}

// TestRecordDeliveryRoundtrip is the end-to-end contract with Task 3: a row
// written by the dispatcher helper is readable through this plugin's
// collection with the shape the UI expects.
func TestRecordDeliveryRoundtrip(t *testing.T) {
	host := newTestHost(t)
	ctx := tenantCtx(snoozetypes.DefaultTenant)

	p := &Plugin{meta: plugins.Metadata{Name: collection}}
	require.NoError(t, p.PostInit(ctx, host))
	require.NoError(t, p.Reload(ctx))

	plugins.RecordDelivery(ctx, host, sampleRow("mail-oncall", "n-1", "a1"))

	docs, total, err := host.DB().Search(ctx, collection, condition.Cond{}, db.Page{})
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Len(t, docs, 1)

	got := docs[0]
	require.NotEmpty(t, got["uid"], "the driver mints the row uid")
	require.Equal(t, "mail-oncall", got["action"])
	require.Equal(t, "mail", got["notifier"])
	require.Equal(t, "success", got["status"])
	require.Equal(t, false, got["batch"])
	require.EqualValues(t, 1757340000, got["date_epoch"])
	require.EqualValues(t, 1, got["alert_count"])
	require.Equal(t, []any{"n-1"}, got["notification_uids"])
	require.Equal(t, []any{"a1"}, got["alert_uids"])

	alerts, ok := got["alerts"].([]any)
	require.True(t, ok)
	require.Len(t, alerts, 1)
	member, ok := alerts[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "db-01", member["host"])
	require.Equal(t, "critical", member["severity"])
}

// TestTenantIsolation: a row written under tenant A is invisible to tenant B.
func TestTenantIsolation(t *testing.T) {
	host := newTestHost(t)
	ctxA := tenantCtx("tenant-a")
	ctxB := tenantCtx("tenant-b")

	plugins.RecordDelivery(ctxA, host, sampleRow("mail-a", "n-a", "a1"))
	plugins.RecordDelivery(ctxB, host, sampleRow("mail-b", "n-b", "b1"))

	docsA, _, err := host.DB().Search(ctxA, collection, condition.Cond{}, db.Page{})
	require.NoError(t, err)
	require.Len(t, docsA, 1)
	require.Equal(t, "mail-a", docsA[0]["action"])

	docsB, _, err := host.DB().Search(ctxB, collection, condition.Cond{}, db.Page{})
	require.NoError(t, err)
	require.Len(t, docsB, 1)
	require.Equal(t, "mail-b", docsB[0]["action"])

	// A naked context must fail closed rather than leak both rows.
	_, _, err = host.DB().Search(context.Background(), collection, condition.Cond{}, db.Page{})
	require.Error(t, err)
}

// TestDSLFilters covers the queries the UI actually issues against the
// collection: the flat-array CONTAINS lookups and the status filter.
func TestDSLFilters(t *testing.T) {
	host := newTestHost(t)
	ctx := tenantCtx(snoozetypes.DefaultTenant)

	plugins.RecordDelivery(ctx, host, sampleRow("mail-oncall", "n-1", "a1"))

	failed := sampleRow("webhook-ops", "n-2", "a2")
	failed.Status = plugins.DeliveryStatusError
	failed.Error = "dial tcp: refused"
	failed.Notifier = "webhook"
	plugins.RecordDelivery(ctx, host, failed)

	cases := []struct {
		name   string
		query  string
		want   int
		action string
	}{
		{"notification uid contains", `notification_uids CONTAINS "n-1"`, 1, "mail-oncall"},
		{"notification uid miss", `notification_uids CONTAINS "nope"`, 0, ""},
		{"alert uid contains", `alert_uids CONTAINS "a2"`, 1, "webhook-ops"},
		{"alert hash contains", `alert_hashes CONTAINS "h-a1"`, 1, "mail-oncall"},
		{"notification name contains", `notification_names CONTAINS "page-oncall"`, 2, ""},
		{"status error", `status = "error"`, 1, "webhook-ops"},
		{"action equals", `action = "mail-oncall"`, 1, "mail-oncall"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cond, err := condition.Parse(tc.query)
			require.NoError(t, err)
			docs, total, err := host.DB().Search(ctx, collection, cond, db.Page{})
			require.NoError(t, err)
			require.Equal(t, tc.want, total)
			if tc.action != "" {
				require.Len(t, docs, 1)
				require.Equal(t, tc.action, docs[0]["action"])
			}
		})
	}
}

// TestDeliveryLogDisabledWritesNothing: the operator kill-switch reaches this
// collection, not just the helper.
func TestDeliveryLogDisabledWritesNothing(t *testing.T) {
	host := newTestHost(t)
	host.cfg.Notification.DeliveryLog = false
	ctx := tenantCtx(snoozetypes.DefaultTenant)

	plugins.RecordDelivery(ctx, host, sampleRow("mail-oncall", "n-1", "a1"))

	_, total, err := host.DB().Search(ctx, collection, condition.Cond{}, db.Page{})
	require.NoError(t, err)
	require.Zero(t, total)
}

// mountedRouter mounts the generic CRUD surface for this plugin and returns a
// helper that issues requests carrying the given permissions.
func mountedRouter(t *testing.T, host *testHost, perms ...string) func(method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	meta, err := plugins.ParseMetadata(metaYAML)
	require.NoError(t, err)
	p := &Plugin{meta: meta}
	require.NoError(t, p.PostInit(context.Background(), host))

	r := chi.NewRouter()
	plugins.MountCRUD(r, host, p)

	return func(method, path, body string) *httptest.ResponseRecorder {
		var rdr io.Reader
		if body != "" {
			rdr = strings.NewReader(body)
		}
		req := httptest.NewRequest(method, path, rdr)
		req.Header.Set("Content-Type", "application/json")
		ctx := auth.WithClaims(req.Context(), snoozetypes.Claims{
			Subject:     "tester",
			Method:      "local",
			Permissions: perms,
		})
		ctx = auth.WithTenant(ctx, snoozetypes.DefaultTenant)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req.WithContext(ctx))
		return rec
	}
}

// TestHTTPWritesForbidden is the security guard for the delivery log. Rows are
// written by the dispatcher straight through the driver; the HTTP create /
// replace / patch verbs that come free with a data-model plugin are a forgery
// primitive — and because the collection sets `audit: false`, a forged or
// rewritten row would leave no trace. Every one of them must 403, even for a
// caller holding rw_notificationlog.
func TestHTTPWritesForbidden(t *testing.T) {
	host := newTestHost(t)
	ctx := tenantCtx(snoozetypes.DefaultTenant)
	plugins.RecordDelivery(ctx, host, sampleRow("mail-oncall", "n-1", "a1"))
	docs, _, err := host.DB().Search(ctx, collection, condition.Cond{}, db.Page{})
	require.NoError(t, err)
	require.Len(t, docs, 1)
	uid, _ := docs[0]["uid"].(string)
	require.NotEmpty(t, uid)

	do := mountedRouter(t, host, "rw_notificationlog")

	forged := `{"status":"success","action":"forged","notifier":"mail"}`
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodPost, "/api/v1/notificationlog", forged},
		{http.MethodPost, "/api/v1/notificationlog", "[" + forged + "]"},
		{http.MethodPut, "/api/v1/notificationlog/" + uid, forged},
		{http.MethodPatch, "/api/v1/notificationlog/" + uid, `{"status":"success"}`},
	} {
		t.Run(tc.method, func(t *testing.T) {
			rec := do(tc.method, tc.path, tc.body)
			require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
			require.Contains(t, rec.Body.String(), "notification dispatcher")
		})
	}

	// Nothing was created or altered: still exactly the one dispatcher row.
	after, _, err := host.DB().Search(ctx, collection, condition.Cond{}, db.Page{})
	require.NoError(t, err)
	require.Len(t, after, 1)
	require.Equal(t, "mail-oncall", after[0]["action"])
}

// TestHTTPDeleteAllowed: DELETE stays mounted (behind rw_notificationlog). The
// e2e harness purges the collection between specs and operators need a way to
// drop history ahead of the retention sweep — deleting is destructive but,
// unlike a forged row, not deceptive.
func TestHTTPDeleteAllowed(t *testing.T) {
	host := newTestHost(t)
	ctx := tenantCtx(snoozetypes.DefaultTenant)
	plugins.RecordDelivery(ctx, host, sampleRow("mail-oncall", "n-1", "a1"))
	plugins.RecordDelivery(ctx, host, sampleRow("mail-oncall", "n-1", "a2"))

	do := mountedRouter(t, host, "rw_notificationlog")

	// Per-uid DELETE — exactly what the e2e harness's clear() loop issues.
	docs, _, err := host.DB().Search(ctx, collection, condition.Cond{}, db.Page{})
	require.NoError(t, err)
	require.Len(t, docs, 2)
	for _, d := range docs {
		uid, _ := d["uid"].(string)
		require.NotEmpty(t, uid)
		rec := do(http.MethodDelete, "/api/v1/notificationlog/"+uid, "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	}

	_, total, err := host.DB().Search(ctx, collection, condition.Cond{}, db.Page{})
	require.NoError(t, err)
	require.Zero(t, total, "the purge must actually delete")

	// Read-only permission is not enough to purge.
	ro := mountedRouter(t, host, "ro_notificationlog")
	require.Equal(t, http.StatusForbidden,
		ro(http.MethodDelete, "/api/v1/notificationlog/whatever", "").Code)
}

// TestHTTPReadRequiresPermission: reads stay behind ro_notificationlog, which
// is why the seeded `notifications` role carries it (see
// core.BackfillNotificationsRolePerms).
func TestHTTPReadRequiresPermission(t *testing.T) {
	host := newTestHost(t)
	plugins.RecordDelivery(tenantCtx(snoozetypes.DefaultTenant), host, sampleRow("mail-oncall", "n-1", "a1"))

	require.Equal(t, http.StatusForbidden,
		mountedRouter(t, host, "rw_notification")(http.MethodGet, "/api/v1/notificationlog", "").Code,
		"rw_notification alone must not read the delivery log")
	require.Equal(t, http.StatusOK,
		mountedRouter(t, host, "ro_notificationlog")(http.MethodGet, "/api/v1/notificationlog", "").Code)
}

// TestHTTPSearchIsARead exercises the AuthzContext.IsRead fix end-to-end
// through the real middleware: POST /<plugin>/search runs the same
// searchHandler as GET ?q=, so a read-only role must be able to call it.
// Before the fix it needed rw_notificationlog — the one permission this
// collection must NOT hand out for reading.
func TestHTTPSearchIsARead(t *testing.T) {
	host := newTestHost(t)
	plugins.RecordDelivery(tenantCtx(snoozetypes.DefaultTenant), host, sampleRow("mail-oncall", "n-1", "a1"))

	body := `{"condition":["=","action","mail-oncall"]}`
	rec := mountedRouter(t, host, "ro_notificationlog")(
		http.MethodPost, "/api/v1/notificationlog/search", body)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "mail-oncall")

	// A caller with no grant at all is still refused.
	require.Equal(t, http.StatusForbidden,
		mountedRouter(t, host, "ro_rule")(http.MethodPost, "/api/v1/notificationlog/search", body).Code)
}
