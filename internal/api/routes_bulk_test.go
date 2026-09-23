package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
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
	"github.com/snoozeweb/snooze/internal/pluginimpl/notification"
	"github.com/snoozeweb/snooze/internal/pluginimpl/notificationlog"
	"github.com/snoozeweb/snooze/internal/pluginimpl/record"
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
	return bulkHarnessWith(t, auditRecord, nil)
}

// bulkHarnessWith is bulkHarness plus extra registered plugins (the map
// overrides same-named stock entries), used by the authorization tests to mount
// the real notificationlog / notification plugins and the hook fakes below
// alongside the stock "record" and "audit" fixtures.
func bulkHarnessWith(t *testing.T, auditRecord bool, extra map[string]plugins.Plugin) (chi.Router, db.Driver) {
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
	for name, p := range extra {
		plugs[name] = p
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

// bulkHookFake is a DataModel over the "record" collection used to exercise
// each branch of guardBulk. It embeds bulkRecordPlugin for the Plugin +
// DataModel surface; the variants below add exactly one optional hook each, so
// a test pins down which hook drives the decision.
type bulkHookFake struct{ *bulkRecordPlugin }

// bulkTransformerFake implements plugins.WriteTransformer only. The hook allows
// everything: the point is that merely CARRYING a per-document transform makes
// the collection non-bulk-writable, because a bulk `set` is one document shared
// by every matched row (savedsearch/comment stamp caller identity into it).
type bulkTransformerFake struct{ bulkHookFake }

func (bulkTransformerFake) TransformWrite(context.Context, map[string]any) error { return nil }

// bulkGuardFake implements plugins.WriteGuard only, and also allows everything:
// a uid-less bulk guard call cannot express the prior-row checks the real
// implementations (user, role, comment, apikey) rely on, so the request is
// refused rather than nominally guarded.
type bulkGuardFake struct{ bulkHookFake }

func (bulkGuardFake) GuardWrite(context.Context, string, map[string]any, bool) error { return nil }

// bulkGuardCall is one recorded GuardBulkWrite invocation.
type bulkGuardCall struct {
	set   db.Document
	tag   []string
	untag []string
}

// bulkOptInFake implements plugins.BulkWriteGuard: the opt-in hook. It records
// every call so a test can assert it fires exactly once per request with the
// request's set/tag/untag, and returns err to exercise the deny path.
type bulkOptInFake struct {
	bulkHookFake
	err   error
	calls []bulkGuardCall
}

func (p *bulkOptInFake) GuardBulkWrite(_ context.Context, set db.Document, tag, untag []string) error {
	p.calls = append(p.calls, bulkGuardCall{set: set, tag: tag, untag: untag})
	return p.err
}

// The hooks are matched by type assertion, so a signature change in the
// interfaces would silently stop these fakes from taking their branch. Assert
// the satisfaction at compile time instead.
var (
	_ plugins.DataModel        = bulkHookFake{}
	_ plugins.WriteTransformer = bulkTransformerFake{}
	_ plugins.WriteGuard       = bulkGuardFake{}
	_ plugins.BulkWriteGuard   = (*bulkOptInFake)(nil)
)

// newBulkOptInFake builds an opt-in fake owning the "record" collection.
func newBulkOptInFake(err error) *bulkOptInFake {
	return &bulkOptInFake{
		bulkHookFake: bulkHookFake{&bulkRecordPlugin{name: recordCollection}},
		err:          err,
	}
}

// TestBulkUpdate_TransformerOnlyPluginRefused: a collection whose plugin
// implements only the per-document WriteTransformer is refused wholesale (403),
// and nothing is written. Running the transform on the shared bulk `set`
// instead would let identity-stamping transforms rewrite the owner/author of
// every matched row.
func TestBulkUpdate_TransformerOnlyPluginRefused(t *testing.T) {
	t.Parallel()
	r, d := bulkHarnessWith(t, false, map[string]plugins.Plugin{
		recordCollection: bulkTransformerFake{bulkHookFake{&bulkRecordPlugin{name: recordCollection}}},
	})
	ctx := snoozetypes.WithTenant(context.Background(), snoozetypes.DefaultTenant)

	_, err := d.Write(ctx, recordCollection, []db.Document{{"host": "h1"}}, db.WriteOptions{})
	require.NoError(t, err)

	rec := bulkReq(t, r, "/api/v1/record/bulk_update",
		map[string]any{"set": map[string]any{"environment": "prod"}}, "rw_record")
	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Contains(t, rec.Body.String(), "does not support bulk writes")

	rows, _, err := d.Search(ctx, recordCollection, noCond(), db.Page{})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Nil(t, rows[0]["environment"])
}

// TestBulkUpdate_GuardOnlyPluginRefused: same refusal for a plugin carrying
// only the per-document WriteGuard, including on a tag-only body (no `set` at
// all), since the guard's contract is per-uid and a bulk call has no uid.
func TestBulkUpdate_GuardOnlyPluginRefused(t *testing.T) {
	t.Parallel()
	r, d := bulkHarnessWith(t, false, map[string]plugins.Plugin{
		recordCollection: bulkGuardFake{bulkHookFake{&bulkRecordPlugin{name: recordCollection}}},
	})
	ctx := snoozetypes.WithTenant(context.Background(), snoozetypes.DefaultTenant)

	_, err := d.Write(ctx, recordCollection, []db.Document{{"host": "h1"}}, db.WriteOptions{})
	require.NoError(t, err)

	rec := bulkReq(t, r, "/api/v1/record/bulk_update",
		map[string]any{"set": map[string]any{"environment": "prod"}}, "rw_record")
	require.Equal(t, http.StatusForbidden, rec.Code)

	rec = bulkReq(t, r, "/api/v1/record/bulk_update",
		map[string]any{"tag": []string{"maint"}}, "rw_record")
	require.Equal(t, http.StatusForbidden, rec.Code)

	rows, _, err := d.Search(ctx, recordCollection, noCond(), db.Page{})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Nil(t, rows[0]["environment"])
	require.Empty(t, tagSet(rows[0]))
}

// TestBulkUpdate_BulkGuardAllows: the opt-in hook is called exactly once per
// request, with the request's set/tag/untag, before any driver call — and when
// it allows, the mutation goes through.
func TestBulkUpdate_BulkGuardAllows(t *testing.T) {
	t.Parallel()
	fake := newBulkOptInFake(nil)
	r, d := bulkHarnessWith(t, false, map[string]plugins.Plugin{recordCollection: fake})
	ctx := snoozetypes.WithTenant(context.Background(), snoozetypes.DefaultTenant)

	_, err := d.Write(ctx, recordCollection, []db.Document{
		{"host": "h1", "tags": []any{"noisy"}},
	}, db.WriteOptions{})
	require.NoError(t, err)

	q := encodeQ(t, condition.Equals("host", "h1"))
	rec := bulkReq(t, r, "/api/v1/record/bulk_update?q="+q, map[string]any{
		"set":   map[string]any{"environment": "prod"},
		"tag":   []string{"maint"},
		"untag": []string{"noisy"},
	}, "rw_record")
	require.Equal(t, http.StatusOK, rec.Code)

	require.Len(t, fake.calls, 1)
	require.Equal(t, db.Document{"environment": "prod"}, fake.calls[0].set)
	require.Equal(t, []string{"maint"}, fake.calls[0].tag)
	require.Equal(t, []string{"noisy"}, fake.calls[0].untag)

	rows, _, err := d.Search(ctx, recordCollection, noCond(), db.Page{})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "prod", rows[0]["environment"])
	require.Contains(t, tagSet(rows[0]), "maint")
	require.NotContains(t, tagSet(rows[0]), "noisy")
}

// TestBulkUpdate_BulkGuardRejects: an error from the opt-in hook is a 403 and
// nothing is written — the hook runs ahead of every driver call.
func TestBulkUpdate_BulkGuardRejects(t *testing.T) {
	t.Parallel()
	fake := newBulkOptInFake(errors.New("not on my collection"))
	r, d := bulkHarnessWith(t, false, map[string]plugins.Plugin{recordCollection: fake})
	ctx := snoozetypes.WithTenant(context.Background(), snoozetypes.DefaultTenant)

	_, err := d.Write(ctx, recordCollection, []db.Document{{"host": "h1"}}, db.WriteOptions{})
	require.NoError(t, err)

	rec := bulkReq(t, r, "/api/v1/record/bulk_update", map[string]any{
		"set": map[string]any{"environment": "prod"},
		"tag": []string{"maint"},
	}, "rw_record")
	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Contains(t, rec.Body.String(), "not on my collection")
	require.Len(t, fake.calls, 1)

	rows, _, err := d.Search(ctx, recordCollection, noCond(), db.Page{})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Nil(t, rows[0]["environment"])
	require.Empty(t, tagSet(rows[0]))
}

// TestBulkUpdate_NoHookPluginProceeds pins the default: a DataModel with none
// of the three hooks keeps the historical hook-free path. This is the case of
// the real `record` plugin, the only collection the web UI bulk-mutates, so
// production behaviour is unchanged by the guard wiring.
func TestBulkUpdate_NoHookPluginProceeds(t *testing.T) {
	t.Parallel()
	r, d := bulkHarness(t, false)
	ctx := snoozetypes.WithTenant(context.Background(), snoozetypes.DefaultTenant)

	_, err := d.Write(ctx, recordCollection, []db.Document{{"host": "h1"}}, db.WriteOptions{})
	require.NoError(t, err)

	rec := bulkReq(t, r, "/api/v1/record/bulk_update",
		map[string]any{"set": map[string]any{"environment": "prod"}}, "rw_record")
	require.Equal(t, http.StatusOK, rec.Code)

	rows, _, err := d.Search(ctx, recordCollection, noCond(), db.Page{})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "prod", rows[0]["environment"])
}

// TestBulkState_HookedPluginRefused: handleBulkState routes through the same
// gate, so a record plugin carrying a per-document hook makes bulk_state 403
// and leaves state untouched. (The real record plugin has no hook — see
// TestBulkState_SetsStateOnMatching for the production path.)
func TestBulkState_HookedPluginRefused(t *testing.T) {
	t.Parallel()
	r, d := bulkHarnessWith(t, false, map[string]plugins.Plugin{
		recordCollection: bulkGuardFake{bulkHookFake{&bulkRecordPlugin{name: recordCollection}}},
	})
	ctx := snoozetypes.WithTenant(context.Background(), snoozetypes.DefaultTenant)

	_, err := d.Write(ctx, recordCollection, []db.Document{{"host": "h1"}}, db.WriteOptions{})
	require.NoError(t, err)

	rec := bulkReq(t, r, "/api/v1/record/bulk_state",
		map[string]any{"state": "ack"}, "rw_record")
	require.Equal(t, http.StatusForbidden, rec.Code)

	rows, _, err := d.Search(ctx, recordCollection, noCond(), db.Page{})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Nil(t, rows[0]["state"])
}

// TestBulkState_BulkGuardAllows: bulk_state hands the state document to the
// opt-in hook and proceeds when it allows.
func TestBulkState_BulkGuardAllows(t *testing.T) {
	t.Parallel()
	fake := newBulkOptInFake(nil)
	r, d := bulkHarnessWith(t, false, map[string]plugins.Plugin{recordCollection: fake})
	ctx := snoozetypes.WithTenant(context.Background(), snoozetypes.DefaultTenant)

	_, err := d.Write(ctx, recordCollection, []db.Document{{"host": "h1"}}, db.WriteOptions{})
	require.NoError(t, err)

	rec := bulkReq(t, r, "/api/v1/record/bulk_state",
		map[string]any{"state": "ack"}, "rw_record")
	require.Equal(t, http.StatusOK, rec.Code)

	require.Len(t, fake.calls, 1)
	// The guard sees the whole SetFields document: the state plus the
	// ownership an ack takes for the caller.
	require.Equal(t, "ack", fake.calls[0].set["state"])
	require.Equal(t, "tester", fake.calls[0].set["owner"])
	require.Nil(t, fake.calls[0].tag)
	require.Nil(t, fake.calls[0].untag)

	rows, _, err := d.Search(ctx, recordCollection, noCond(), db.Page{})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "ack", rows[0]["state"])
}

// TestBulkUpdate_NotificationPluginNotMutable documents why the notification
// plugin's TransformWrite never mattered on this path: notification.Plugin is
// a Processor, not a plugins.DataModel, so bulk_update 404s on it before any
// hook decision is reached.
func TestBulkUpdate_NotificationPluginNotMutable(t *testing.T) {
	t.Parallel()
	r, _ := bulkHarnessWith(t, false, map[string]plugins.Plugin{
		"notification": &notification.Plugin{},
	})

	rec := bulkReq(t, r, "/api/v1/notification/bulk_update",
		map[string]any{"set": map[string]any{"enabled": false}}, "rw_notification")
	require.Equal(t, http.StatusNotFound, rec.Code)
}

// TestBulkUpdate_NotificationLogRefusesSet: notificationlog implements the
// per-document WriteGuard and has not opted into bulk writes, so a bulk_update
// `set` must 403 and leave the delivery rows byte-identical. Without the gate
// this call rewrote a failed delivery as a successful one with no audit trail
// (the collection is audit:false).
func TestBulkUpdate_NotificationLogRefusesSet(t *testing.T) {
	t.Parallel()
	r, d := bulkHarnessWith(t, false, map[string]plugins.Plugin{
		plugins.NotificationLogCollection: &notificationlog.Plugin{},
	})
	ctx := snoozetypes.WithTenant(context.Background(), snoozetypes.DefaultTenant)

	_, err := d.Write(ctx, plugins.NotificationLogCollection, []db.Document{
		{"action": "mail", "status": "error", "error": "smtp refused"},
	}, db.WriteOptions{})
	require.NoError(t, err)

	rec := bulkReq(t, r, "/api/v1/"+plugins.NotificationLogCollection+"/bulk_update",
		map[string]any{"set": map[string]any{"status": "success", "error": ""}},
		"rw_"+plugins.NotificationLogCollection)
	require.Equal(t, http.StatusForbidden, rec.Code)

	rows, _, err := d.Search(ctx, plugins.NotificationLogCollection, noCond(), db.Page{})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "error", rows[0]["status"])
	require.Equal(t, "smtp refused", rows[0]["error"])
}

// TestBulkUpdate_NotificationLogRefusesTagOnly: the refusal is per REQUEST,
// not per field, so a tag-only or untag-only body — which touches no field in
// `set` — is blocked by the same decision.
func TestBulkUpdate_NotificationLogRefusesTagOnly(t *testing.T) {
	t.Parallel()
	r, d := bulkHarnessWith(t, false, map[string]plugins.Plugin{
		plugins.NotificationLogCollection: &notificationlog.Plugin{},
	})
	ctx := snoozetypes.WithTenant(context.Background(), snoozetypes.DefaultTenant)

	_, err := d.Write(ctx, plugins.NotificationLogCollection, []db.Document{
		{"action": "mail", "status": "error"},
	}, db.WriteOptions{})
	require.NoError(t, err)

	rec := bulkReq(t, r, "/api/v1/"+plugins.NotificationLogCollection+"/bulk_update",
		map[string]any{"tag": []string{"forged"}},
		"rw_"+plugins.NotificationLogCollection)
	require.Equal(t, http.StatusForbidden, rec.Code)

	rows, _, err := d.Search(ctx, plugins.NotificationLogCollection, noCond(), db.Page{})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Empty(t, tagSet(rows[0]))

	// untag-only is guarded by the same single call.
	rec = bulkReq(t, r, "/api/v1/"+plugins.NotificationLogCollection+"/bulk_update",
		map[string]any{"untag": []string{"forged"}},
		"rw_"+plugins.NotificationLogCollection)
	require.Equal(t, http.StatusForbidden, rec.Code)
}

// TestBulkUpdate_EmitsAuditRowPerUID guards the regression risk of the bulk
// authorization gate: an audited collection whose plugin has no hooks still
// mutates and still writes one audit row per affected uid.
func TestBulkUpdate_EmitsAuditRowPerUID(t *testing.T) {
	t.Parallel()
	r, d := bulkHarness(t, true)
	ctx := snoozetypes.WithTenant(context.Background(), snoozetypes.DefaultTenant)

	_, err := d.Write(ctx, "record", []db.Document{
		{"host": "h1"},
		{"host": "h1"},
		{"host": "h2"},
	}, db.WriteOptions{})
	require.NoError(t, err)

	q := encodeQ(t, condition.Equals("host", "h1"))
	rec := bulkReq(t, r, "/api/v1/record/bulk_update?q="+q, map[string]any{
		"set": map[string]any{"environment": "prod"},
		"tag": []string{"maint"},
	}, "rw_record")
	require.Equal(t, http.StatusOK, rec.Code)

	rows, _, err := d.Search(ctx, "audit", noCond(), db.Page{})
	require.NoError(t, err)
	require.Len(t, rows, 2)
	for _, row := range rows {
		require.Equal(t, "record", row["object_type"])
		require.Equal(t, "bulk_update", row["action"])
		require.Equal(t, "set; tag", row["summary"])
		require.NotEmpty(t, row["object_id"])
	}
}

// The REAL record plugin must stay a DataModel — bulk_update rejects anything
// that is not one.
var _ plugins.DataModel = (*record.Plugin)(nil)

// TestRecordPluginStaysBulkWritable is a regression guard on the alerts UI,
// asserted against the REAL plugin rather than a stand-in.
//
// guardBulk refuses a bulk request outright (403) when the collection's plugin
// implements a per-document write hook without opting into
// plugins.BulkWriteGuard — the per-document hooks cannot be honoured by a
// query-wide mutation, so half-checking is worse than refusing. `record` is
// the collection the web UI bulk-mutates: every "ack selected", "close
// selected" and bulk tag/untag in the alerts table goes through
// bulk_state/bulk_update on it. The moment somebody adds a GuardWrite or a
// TransformWrite to the record plugin — a reasonable-looking change, e.g. to
// stamp an editor — those buttons all start returning 403.
//
// So this pins the negative: the real record.Plugin implements NEITHER hook,
// and guardBulk therefore takes its no-hook path. The other tests in this file
// use fakes, which cannot catch a change to the real plugin. If this test ever
// fails, the fix is not to delete it: it is to implement GuardBulkWrite on the
// record plugin with semantics chosen deliberately for a whole-query write.
func TestRecordPluginStaysBulkWritable(t *testing.T) {
	var p plugins.Plugin = &record.Plugin{}

	_, isGuard := p.(plugins.WriteGuard)
	require.False(t, isGuard,
		"record must not implement WriteGuard, or every alerts-table bulk action 403s")
	_, isTransformer := p.(plugins.WriteTransformer)
	require.False(t, isTransformer,
		"record must not implement WriteTransformer, or every alerts-table bulk action 403s")
	_, isBulkGuard := p.(plugins.BulkWriteGuard)
	require.False(t, isBulkGuard,
		"record has no bulk guard today; adding one is fine but must be a deliberate change")

	// End to end through the gate itself, so the assertion above cannot drift
	// away from what the handler actually does.
	rt := &Router{}
	for _, tc := range []struct {
		name string
		set  db.Document
		tag  []string
	}{
		{"state change (bulk_state)", db.Document{"state": "ack"}, nil},
		{"tag only (bulk_update)", nil, []string{"noisy"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodPost, "/api/v1/record/bulk_update", nil)
			require.True(t, rt.guardBulk(w, r, p, tc.set, tc.tag, nil),
				"the real record plugin must pass the bulk gate")
			require.Empty(t, w.Body.String(), "the gate must not have written a response")
		})
	}
}
