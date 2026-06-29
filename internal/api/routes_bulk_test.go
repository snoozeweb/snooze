package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/config"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/db/sqlite"
	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/internal/telemetry"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// bulkRecordPlugin is a minimal plugins.DataModel standing in for the real
// record plugin: it owns the "record" collection and (with audit enabled in
// the test that needs it) drives EmitBulkAudit. It is the {plugin} resolved by
// handleBulkUpdate.
type bulkRecordPlugin struct {
	name  string
	audit bool
}

func (p *bulkRecordPlugin) Name() string { return p.name }
func (p *bulkRecordPlugin) Metadata() plugins.Metadata {
	return plugins.Metadata{Name: p.name, Audit: p.audit}
}
func (p *bulkRecordPlugin) PostInit(context.Context, plugins.Host) error { return nil }
func (p *bulkRecordPlugin) Reload(context.Context) error                 { return nil }
func (p *bulkRecordPlugin) Schema() any                                  { return map[string]any{} }
func (p *bulkRecordPlugin) Validate(map[string]any) error                { return nil }

// bulkPlainPlugin satisfies plugins.Plugin but NOT plugins.DataModel, so
// handleBulkUpdate must reject it (only DataModel collections are mutable).
type bulkPlainPlugin struct{ name string }

func (p *bulkPlainPlugin) Name() string                                 { return p.name }
func (p *bulkPlainPlugin) Metadata() plugins.Metadata                   { return plugins.Metadata{Name: p.name} }
func (p *bulkPlainPlugin) PostInit(context.Context, plugins.Host) error { return nil }
func (p *bulkPlainPlugin) Reload(context.Context) error                 { return nil }

// bulkTestHost is a tiny plugins.Host backed by the SQLite test driver and the
// plugin map, used so the bulk handlers can resolve {plugin} and emit audit
// rows through host.DB().
type bulkTestHost struct {
	driver db.Driver
	plugs  map[string]plugins.Plugin
}

func (h *bulkTestHost) DB() db.Driver                { return h.driver }
func (h *bulkTestHost) Bus() plugins.Bus             { return nil }
func (h *bulkTestHost) Logger() *slog.Logger         { return slog.Default() }
func (h *bulkTestHost) Tracer() trace.Tracer         { return otel.Tracer("bulk-test") }
func (h *bulkTestHost) Metrics() *telemetry.Registry { return telemetry.NewRegistry(nil) }
func (h *bulkTestHost) Config() *config.Config       { return config.Default() }
func (h *bulkTestHost) Plugin(name string) plugins.Plugin {
	return h.plugs[name]
}

// bulkHarness brings up a SQLite-backed Router wired with mountBulk, mirroring
// retroApplyHarness. The "record" plugin is registered as a DataModel so
// bulk_update can resolve it. auditRecord toggles whether the record plugin
// opts into auditing.
func bulkHarness(t *testing.T, auditRecord bool) (chi.Router, db.Driver) {
	t.Helper()
	ctx := snoozetypes.WithTenant(context.Background(), snoozetypes.DefaultTenant)
	d, err := sqlite.New(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "snooze.db")})
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })

	plugs := map[string]plugins.Plugin{
		"record": &bulkRecordPlugin{name: "record", audit: auditRecord},
		// a non-DataModel plugin to exercise the rejection path
		"audit": &bulkPlainPlugin{name: "audit"},
	}
	host := &bulkTestHost{driver: d, plugs: plugs}
	rt := &Router{DB: d, Host: host, Plugins: plugs}
	r := chi.NewRouter()
	rt.mountBulk(r)
	return r, d
}

// encodeQ base64url-encodes a condition for the ?q parameter, matching the
// decodeListParams/decodeQueryCond convention.
func encodeQ(t *testing.T, c condition.Cond) string {
	t.Helper()
	raw, err := json.Marshal(c)
	require.NoError(t, err)
	return base64.RawURLEncoding.EncodeToString(raw)
}

// bulkReq issues an authenticated bulk request with the given perms.
func bulkReq(t *testing.T, r chi.Router, target string, body any, perms ...string) *httptest.ResponseRecorder {
	t.Helper()
	var buf []byte
	if body != nil {
		var err error
		buf, err = json.Marshal(body)
		require.NoError(t, err)
	}
	req := authReq("POST", target, buf, perms...)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestBulkState_SetsStateOnMatching(t *testing.T) {
	t.Parallel()
	r, d := bulkHarness(t, false)
	ctx := snoozetypes.WithTenant(context.Background(), snoozetypes.DefaultTenant)

	_, err := d.Write(ctx, "record", []db.Document{
		{"host": "noisy-1", "message": "one"},
		{"host": "noisy-1", "message": "two"},
		{"host": "quiet-1", "message": "skip"},
	}, db.WriteOptions{})
	require.NoError(t, err)

	q := encodeQ(t, condition.Equals("host", "noisy-1"))
	rec := bulkReq(t, r, "/api/v1/record/bulk_state?q="+q,
		map[string]any{"state": "ack", "message": "silenced for maint"}, "rw_record")
	require.Equal(t, http.StatusOK, rec.Code)

	var body bulkStateResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, 2, body.Matched)
	require.Equal(t, 2, body.Updated)
	require.Equal(t, "ack", body.State)

	// Exactly the two matching records now have state:"ack".
	all, _, err := d.Search(ctx, "record", noCond(), db.Page{})
	require.NoError(t, err)
	var acked, other int
	for _, doc := range all {
		if doc["state"] == "ack" {
			acked++
		} else {
			other++
		}
	}
	require.Equal(t, 2, acked)
	require.Equal(t, 1, other)
}

// TestBulkState_EmitsOneAuditRowPerUID exercises the audited path end-to-end:
// with the record plugin's meta.Audit enabled, a bulk_state over a 2-match
// query writes exactly one audit row per affected uid, carrying the action and
// the message-derived summary.
func TestBulkState_EmitsOneAuditRowPerUID(t *testing.T) {
	t.Parallel()
	r, d := bulkHarness(t, true)
	ctx := snoozetypes.WithTenant(context.Background(), snoozetypes.DefaultTenant)

	_, err := d.Write(ctx, "record", []db.Document{
		{"host": "noisy-1"},
		{"host": "noisy-1"},
		{"host": "quiet-1"},
	}, db.WriteOptions{})
	require.NoError(t, err)

	q := encodeQ(t, condition.Equals("host", "noisy-1"))
	rec := bulkReq(t, r, "/api/v1/record/bulk_state?q="+q,
		map[string]any{"state": "ack", "message": "maint window"}, "rw_record")
	require.Equal(t, http.StatusOK, rec.Code)

	rows, _, err := d.Search(ctx, "audit", noCond(), db.Page{})
	require.NoError(t, err)
	require.Len(t, rows, 2)
	for _, row := range rows {
		require.Equal(t, "record", row["object_type"])
		require.Equal(t, "bulk_state", row["action"])
		require.Equal(t, "ack: maint window", row["summary"])
		require.NotEmpty(t, row["object_id"])
	}
}

func TestBulkState_RejectsUnknownState(t *testing.T) {
	t.Parallel()
	r, _ := bulkHarness(t, false)

	rec := bulkReq(t, r, "/api/v1/record/bulk_state",
		map[string]any{"state": "frobnicate"}, "rw_record")
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

// TestBulkState_RequiresRwRecord mirrors TestRetroApply_RequiresRwRecord:
// ro_record → 403, rw_all → 200, no claims → 401.
func TestBulkState_RequiresRwRecord(t *testing.T) {
	t.Parallel()
	r, _ := bulkHarness(t, false)
	body, _ := json.Marshal(map[string]any{"state": "ack"})

	// Only ro_record → 403.
	req := authReq("POST", "/api/v1/record/bulk_state", body, "ro_record")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code)

	// rw_all (admin wildcard) → 200.
	req = authReq("POST", "/api/v1/record/bulk_state", body, "rw_all")
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	// No claims → 401.
	req = httptest.NewRequest(http.MethodPost, "/api/v1/record/bulk_state", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestBulkUpdate_SetTagUntag(t *testing.T) {
	t.Parallel()
	r, d := bulkHarness(t, false)
	ctx := snoozetypes.WithTenant(context.Background(), snoozetypes.DefaultTenant)

	_, err := d.Write(ctx, "record", []db.Document{
		{"host": "h1", "tags": []any{"noisy"}},
		{"host": "h1", "tags": []any{"noisy"}},
		{"host": "h2", "tags": []any{"noisy"}},
	}, db.WriteOptions{})
	require.NoError(t, err)

	q := encodeQ(t, condition.Equals("host", "h1"))
	rec := bulkReq(t, r, "/api/v1/record/bulk_update?q="+q, map[string]any{
		"set":   map[string]any{"environment": "prod"},
		"tag":   []string{"maint"},
		"untag": []string{"noisy"},
	}, "rw_record")
	require.Equal(t, http.StatusOK, rec.Code)

	var body bulkUpdateResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, 2, body.Matched)

	all, _, err := d.Search(ctx, "record", condition.Equals("host", "h1"), db.Page{})
	require.NoError(t, err)
	require.Len(t, all, 2)
	for _, doc := range all {
		require.Equal(t, "prod", doc["environment"])
		require.Contains(t, tagSet(doc), "maint")
		require.NotContains(t, tagSet(doc), "noisy")
	}

	// The non-matching record keeps its noisy tag and gains no environment.
	h2, _, err := d.Search(ctx, "record", condition.Equals("host", "h2"), db.Page{})
	require.NoError(t, err)
	require.Len(t, h2, 1)
	require.Contains(t, tagSet(h2[0]), "noisy")
	require.Nil(t, h2[0]["environment"])
}

func TestBulkUpdate_TagIsIdempotent(t *testing.T) {
	t.Parallel()
	r, d := bulkHarness(t, false)
	ctx := snoozetypes.WithTenant(context.Background(), snoozetypes.DefaultTenant)

	_, err := d.Write(ctx, "record", []db.Document{
		{"host": "h1"},
	}, db.WriteOptions{})
	require.NoError(t, err)

	q := encodeQ(t, condition.Equals("host", "h1"))
	body := map[string]any{"tag": []string{"maint"}}

	rec := bulkReq(t, r, "/api/v1/record/bulk_update?q="+q, body, "rw_record")
	require.Equal(t, http.StatusOK, rec.Code)
	rec = bulkReq(t, r, "/api/v1/record/bulk_update?q="+q, body, "rw_record")
	require.Equal(t, http.StatusOK, rec.Code)

	all, _, err := d.Search(ctx, "record", condition.Equals("host", "h1"), db.Page{})
	require.NoError(t, err)
	require.Len(t, all, 1)
	// Exactly one "maint" even after tagging twice.
	var count int
	for _, tag := range tagSet(all[0]) {
		if tag == "maint" {
			count++
		}
	}
	require.Equal(t, 1, count)
}

func TestBulkUpdate_RejectsEmptyBody(t *testing.T) {
	t.Parallel()
	r, _ := bulkHarness(t, false)

	rec := bulkReq(t, r, "/api/v1/record/bulk_update", map[string]any{}, "rw_record")
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestBulkUpdate_RejectsNonDataModelPlugin(t *testing.T) {
	t.Parallel()
	r, _ := bulkHarness(t, false)

	// "audit" is a registered plugin but not a DataModel → reject.
	rec := bulkReq(t, r, "/api/v1/audit/bulk_update",
		map[string]any{"set": map[string]any{"x": 1}}, "rw_audit")
	require.True(t, rec.Code == http.StatusNotFound || rec.Code == http.StatusBadRequest,
		"non-DataModel plugin should be rejected, got %d", rec.Code)

	// An entirely unknown plugin → reject too.
	rec = bulkReq(t, r, "/api/v1/nope/bulk_update",
		map[string]any{"set": map[string]any{"x": 1}}, "rw_nope")
	require.True(t, rec.Code == http.StatusNotFound || rec.Code == http.StatusBadRequest,
		"unknown plugin should be rejected, got %d", rec.Code)
}

// tagSet returns the record's tags as a []string regardless of whether the
// driver hands them back as []any or []string.
func tagSet(doc db.Document) []string {
	switch v := doc["tags"].(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, e := range v {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}
