package snooze

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
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
	"github.com/snoozeweb/snooze/internal/db/asyncwriter"
	"github.com/snoozeweb/snooze/internal/db/sqlite"
	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/internal/syncer"
	"github.com/snoozeweb/snooze/internal/telemetry"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// capturedInc records one BulkIncrement operation for assertion in tests.
type capturedInc struct {
	metric string
	dim    string
	key    string
	bucket int64
	delta  int64
}

// captureDrv is a no-op db.Driver whose only live method is BulkIncrement;
// it records every op into the shared slice pointed to by calls.
type captureDrv struct {
	mu    sync.Mutex
	calls *[]capturedInc
}

func newCaptureDrv() (*captureDrv, *[]capturedInc) {
	calls := &[]capturedInc{}
	return &captureDrv{calls: calls}, calls
}

func (d *captureDrv) BulkIncrement(_ context.Context, _ string, ops []db.IncrementOp, _ bool) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, op := range ops {
		metric, _ := op.Search["metric"].(string)
		dim, _ := op.Search["dim"].(string)
		key, _ := op.Search["key"].(string)
		bucket, _ := op.Search["bucket"].(int64)
		for _, delta := range op.Deltas {
			*d.calls = append(*d.calls, capturedInc{
				metric: metric,
				dim:    dim,
				key:    key,
				bucket: bucket,
				delta:  delta,
			})
		}
	}
	return nil
}

// Remaining Driver stubs — none are called by the asyncwriter.Writer path.
func (d *captureDrv) Search(context.Context, string, condition.Cond, db.Page) ([]db.Document, int, error) {
	return nil, 0, nil
}
func (d *captureDrv) GetOne(context.Context, string, db.Document) (db.Document, error) {
	return nil, nil
}
func (d *captureDrv) Write(context.Context, string, []db.Document, db.WriteOptions) (db.WriteResult, error) {
	return db.WriteResult{}, nil
}
func (d *captureDrv) ReplaceOne(context.Context, string, db.Document, db.Document, bool) (int, error) {
	return 0, nil
}
func (d *captureDrv) UpdateOne(context.Context, string, string, db.Document, bool) error {
	return nil
}
func (d *captureDrv) Delete(context.Context, string, condition.Cond, bool) (int, error) {
	return 0, nil
}
func (d *captureDrv) Convert(context.Context, condition.Cond, []string) (db.DriverQuery, error) {
	return nil, nil
}
func (d *captureDrv) IncMany(context.Context, string, string, condition.Cond, int64) (int, error) {
	return 0, nil
}
func (d *captureDrv) SetFields(context.Context, string, db.Document, condition.Cond) (int, error) {
	return 0, nil
}
func (d *captureDrv) UnsetFields(context.Context, string, []string, condition.Cond) (int, error) {
	return 0, nil
}
func (d *captureDrv) AppendList(context.Context, string, map[string][]any, condition.Cond) (int, error) {
	return 0, nil
}
func (d *captureDrv) PrependList(context.Context, string, map[string][]any, condition.Cond) (int, error) {
	return 0, nil
}
func (d *captureDrv) RemoveList(context.Context, string, map[string][]any, condition.Cond) (int, error) {
	return 0, nil
}
func (d *captureDrv) CreateIndex(context.Context, string, []string) error { return nil }
func (d *captureDrv) ListCollections(context.Context) ([]string, error)   { return nil, nil }
func (d *captureDrv) Drop(context.Context, string) error                  { return nil }
func (d *captureDrv) Backup(context.Context, string, []string) error      { return nil }
func (d *captureDrv) CleanupTimeout(context.Context, string) (int, error) { return 0, nil }
func (d *captureDrv) CleanupComments(context.Context) (int, error)        { return 0, nil }
func (d *captureDrv) CleanupOrphans(context.Context, string) (int, error) { return 0, nil }
func (d *captureDrv) CleanupAuditLogs(context.Context, time.Duration) (int, error) {
	return 0, nil
}
func (d *captureDrv) CleanupSnooze(context.Context) (int, error)       { return 0, nil }
func (d *captureDrv) CleanupNotification(context.Context) (int, error) { return 0, nil }
func (d *captureDrv) ComputeStats(context.Context, string, time.Time, time.Time, string) ([]db.StatsBucket, error) {
	return nil, nil
}
func (d *captureDrv) Watcher() syncer.Bus { return nil }
func (d *captureDrv) Close() error        { return nil }

// stubHost is a Host that only wires the bits the snooze plugin reads: the
// driver, a logger, the metrics registry, the OTEL tracer and the immutable
// config. Bus is unused; sibling-plugin lookup is unused.
// asyncWriter is optional; when set the host also satisfies AsyncWriterHost.
type stubHost struct {
	driver      db.Driver
	logger      *slog.Logger
	cfg         *config.Config
	metr        *telemetry.Registry
	tracer      trace.Tracer
	asyncWriter *asyncwriter.Writer
	runtimeSet  *config.RuntimeSettings
}

func newStubHost(t *testing.T) *stubHost {
	t.Helper()
	path := filepath.Join(t.TempDir(), "snooze.db")
	d, err := sqlite.New(context.Background(), sqlite.Config{Path: path})
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	return &stubHost{
		driver: d,
		logger: slog.Default(),
		cfg:    config.Default(),
		metr:   telemetry.NewRegistry(nil),
		tracer: otel.Tracer("snooze-plugin-test"),
	}
}

func (h *stubHost) DB() db.Driver                { return h.driver }
func (h *stubHost) Bus() plugins.Bus             { return nil }
func (h *stubHost) Logger() *slog.Logger         { return h.logger }
func (h *stubHost) Tracer() trace.Tracer         { return h.tracer }
func (h *stubHost) Metrics() *telemetry.Registry { return h.metr }
func (h *stubHost) Config() *config.Config       { return h.cfg }
func (h *stubHost) Plugin(string) plugins.Plugin { return nil }

// AsyncWriter satisfies plugins.AsyncWriterHost when an asyncWriter is set.
func (h *stubHost) AsyncWriter() *asyncwriter.Writer { return h.asyncWriter }

// RuntimeSettings satisfies plugins.RuntimeSettingsHost when runtimeSet is
// set. Most tests leave it nil, exercising the file-config fallback path in
// bypassSeverities.
func (h *stubHost) RuntimeSettings() *config.RuntimeSettings { return h.runtimeSet }

// writeRule inserts a snooze record built from a free-form Document. Returns
// the assigned uid so tests can poke at the row afterwards.
func writeRule(t *testing.T, h *stubHost, doc db.Document) string {
	t.Helper()
	ctx := auth.WithTenant(context.Background(), snoozetypes.DefaultTenant)
	res, err := h.driver.Write(ctx, "snooze",
		[]db.Document{doc}, db.WriteOptions{Primary: []string{"name"}, UpdateTime: true})
	require.NoError(t, err)
	require.Len(t, res.Added, 1)
	return res.Added[0]
}

// newPlugin builds a Plugin wired to h, with an injectable clock.
func newPlugin(t *testing.T, h *stubHost, now func() time.Time) *Plugin {
	t.Helper()
	ctx := auth.WithTenant(context.Background(), snoozetypes.DefaultTenant)
	p := &Plugin{Now: now}
	require.NoError(t, p.PostInit(ctx, h))
	return p
}

// TestSnoozeMatch_AbortWrite matches the Python `test_snooze_1`: condition
// matches, no `discard`, so the plugin returns ActionAbortWrite and tags the
// record with the rule name.
func TestSnoozeMatch_AbortWrite(t *testing.T) {
	t.Parallel()
	h := newStubHost(t)
	writeRule(t, h, db.Document{
		"name":      "Filter 1",
		"condition": []any{"=", "a", "1"},
	})
	p := newPlugin(t, h, nil)

	ctx := auth.WithTenant(context.Background(), snoozetypes.DefaultTenant)
	rec := snoozetypes.Record{Extra: map[string]any{"a": "1", "b": "2"}}
	res, err := p.Process(ctx, rec)
	require.NoError(t, err)
	require.Equal(t, plugins.ActionAbortWrite, res.Action)
	require.Equal(t, "Filter 1", res.Record.Extra["snoozed"])
}

// TestSnoozeBypassSeverity covers the global general.snooze_bypass_severities
// early-return: a record whose (case-folded) severity is in the bypass list
// passes straight through with ActionContinue — before any rule is even tested
// — while a non-bypassed severity still trips the catch-all rule.
func TestSnoozeBypassSeverity(t *testing.T) {
	t.Parallel()
	h := newStubHost(t)
	h.cfg.General.SnoozeBySeverities = []string{"ok"}
	// Catch-all rule: no condition → matches every record.
	writeRule(t, h, db.Document{"name": "catch-all"})
	p := newPlugin(t, h, nil)
	ctx := auth.WithTenant(context.Background(), snoozetypes.DefaultTenant)

	// severity "OK" → bypassed; the catch-all rule must NOT fire.
	res, err := p.Process(ctx, snoozetypes.Record{Severity: "OK"})
	require.NoError(t, err)
	require.Equal(t, plugins.ActionContinue, res.Action)
	require.Nil(t, res.Record.Extra["snoozed"])

	// severity "critical" → not in the bypass list; catch-all must fire.
	res2, err := p.Process(ctx, snoozetypes.Record{Severity: "critical"})
	require.NoError(t, err)
	require.NotEqual(t, plugins.ActionContinue, res2.Action)
}

// TestSnoozeMiss_Continue matches the Python `test_snooze_2`: no rule
// matches, the plugin votes Continue and leaves the record alone.
func TestSnoozeMiss_Continue(t *testing.T) {
	t.Parallel()
	h := newStubHost(t)
	writeRule(t, h, db.Document{
		"name":      "Filter 1",
		"condition": []any{"=", "a", "1"},
	})
	writeRule(t, h, db.Document{
		"name":      "Filter 2",
		"condition": []any{"=", "a", "3"},
		"discard":   true,
	})
	p := newPlugin(t, h, nil)

	ctx := auth.WithTenant(context.Background(), snoozetypes.DefaultTenant)
	rec := snoozetypes.Record{Extra: map[string]any{"a": "2", "b": "2"}}
	res, err := p.Process(ctx, rec)
	require.NoError(t, err)
	require.Equal(t, plugins.ActionContinue, res.Action)
	require.Nil(t, res.Record.Extra["snoozed"])
}

// TestSnoozeDiscard_Abort matches the Python `test_snooze_3`: a discard rule
// matches, the plugin returns ActionAbort (drop without persisting).
func TestSnoozeDiscard_Abort(t *testing.T) {
	t.Parallel()
	h := newStubHost(t)
	writeRule(t, h, db.Document{
		"name":      "Filter 2",
		"condition": []any{"=", "a", "3"},
		"discard":   true,
	})
	p := newPlugin(t, h, nil)

	ctx := auth.WithTenant(context.Background(), snoozetypes.DefaultTenant)
	rec := snoozetypes.Record{Extra: map[string]any{"a": "3", "b": "2"}}
	res, err := p.Process(ctx, rec)
	require.NoError(t, err)
	require.Equal(t, plugins.ActionAbort, res.Action)
	require.Equal(t, "Filter 2", res.Record.Extra["snoozed"])
}

// TestSnoozeDisabled covers a defaulted-on rule explicitly disabled: it must
// not fire even when the condition would otherwise match.
func TestSnoozeDisabled(t *testing.T) {
	t.Parallel()
	h := newStubHost(t)
	writeRule(t, h, db.Document{
		"name":      "Filter 1",
		"condition": []any{"=", "a", "1"},
		"enabled":   false,
	})
	p := newPlugin(t, h, nil)

	ctx := auth.WithTenant(context.Background(), snoozetypes.DefaultTenant)
	rec := snoozetypes.Record{Extra: map[string]any{"a": "1"}}
	res, err := p.Process(ctx, rec)
	require.NoError(t, err)
	require.Equal(t, plugins.ActionContinue, res.Action)
}

// TestSnoozeTimeConstraints covers the time-constraint gate. Wednesday
// 12:00 should match a Mon-Thu window; Saturday should not.
func TestSnoozeTimeConstraints(t *testing.T) {
	t.Parallel()
	h := newStubHost(t)
	writeRule(t, h, db.Document{
		"name":      "Snooze rule 1",
		"condition": []any{"=", "host", "myhost01"},
		"time_constraints": map[string]any{
			"weekdays": []any{
				map[string]any{"weekdays": []any{1, 2, 3, 4}},
			},
			"time": []any{
				map[string]any{"from": "10:00", "until": "14:00"},
			},
		},
	})

	ctx := auth.WithTenant(context.Background(), snoozetypes.DefaultTenant)
	rec := snoozetypes.Record{Host: "myhost01"}

	// 2021-07-07 was a Wednesday (weekday 3).
	wed := time.Date(2021, 7, 7, 12, 0, 0, 0, time.UTC)
	p := newPlugin(t, h, func() time.Time { return wed })
	res, err := p.Process(ctx, rec)
	require.NoError(t, err)
	require.Equal(t, plugins.ActionAbortWrite, res.Action)
	require.Equal(t, "Snooze rule 1", res.Record.Extra["snoozed"])

	// 2021-07-10 was a Saturday (weekday 6) - outside the window.
	sat := time.Date(2021, 7, 10, 12, 0, 0, 0, time.UTC)
	p2 := newPlugin(t, h, func() time.Time { return sat })
	res2, err := p2.Process(ctx, rec)
	require.NoError(t, err)
	require.Equal(t, plugins.ActionContinue, res2.Action)
}

// TestSnoozeReload exercises the cache-refresh path: a rule added after
// PostInit becomes effective after a Reload call.
func TestSnoozeReload(t *testing.T) {
	t.Parallel()
	h := newStubHost(t)
	ctx := auth.WithTenant(context.Background(), snoozetypes.DefaultTenant)
	p := newPlugin(t, h, nil)
	require.Empty(t, p.cachedRules(snoozetypes.DefaultTenant))

	writeRule(t, h, db.Document{
		"name":      "Filter 1",
		"condition": []any{"=", "a", "1"},
	})

	// Stale cache: no match yet.
	rec := snoozetypes.Record{Extra: map[string]any{"a": "1"}}
	res, err := p.Process(ctx, rec)
	require.NoError(t, err)
	require.Equal(t, plugins.ActionContinue, res.Action)

	// After Reload the new rule is picked up.
	require.NoError(t, p.Reload(ctx))
	require.Len(t, p.cachedRules(snoozetypes.DefaultTenant), 1)
	res, err = p.Process(ctx, rec)
	require.NoError(t, err)
	require.Equal(t, plugins.ActionAbortWrite, res.Action)
}

// TestSnoozeReloadOrder_OlderRuleWinsOverNewerOverlapping guards a real
// production incident: an older, specific discard rule ("silence this host
// entirely") was being silently shadowed by an unrelated tag rule created
// months later, because Reload loaded rules in DESCENDING order (an empty
// db.Page{} defaults to that on every driver — Page.Asc's zero value is
// false) instead of insertion order. Process is first-match-wins, so load
// order is priority order: the older rule must be tried first.
func TestSnoozeReloadOrder_OlderRuleWinsOverNewerOverlapping(t *testing.T) {
	t.Parallel()
	h := newStubHost(t)

	writeRule(t, h, db.Document{
		"name":      "Older discard rule",
		"condition": []any{"=", "host", "K8S staging"},
		"discard":   true,
	})
	// Created after the rule above, and its condition also matches the same
	// record — mirrors the live incident where a later, broader tag rule
	// (e.g. "K8S Backups", host MATCHES "K8S.*") intercepted alerts an
	// older host-specific discard rule was meant to own outright.
	writeRule(t, h, db.Document{
		"name":      "Newer overlapping tag rule",
		"condition": []any{"=", "host", "K8S staging"},
	})

	p := newPlugin(t, h, nil)
	ctx := auth.WithTenant(context.Background(), snoozetypes.DefaultTenant)
	rec := snoozetypes.Record{Host: "K8S staging"}
	res, err := p.Process(ctx, rec)
	require.NoError(t, err)
	require.Equal(t, plugins.ActionAbort, res.Action,
		"the older, more specific rule must win — not the newer overlapping one")
}

// TestSnooze_TenantIsolation verifies that a snooze rule for tenant A does not
// affect records processed under tenant B.
func TestSnooze_TenantIsolation(t *testing.T) {
	t.Parallel()
	h := newStubHost(t)

	ctxA := auth.WithTenant(context.Background(), "acme")
	ctxB := auth.WithTenant(context.Background(), "beta")

	_, err := h.DB().Write(ctxA, "snooze", []db.Document{{
		"name":    "silence-all",
		"enabled": true,
	}}, db.WriteOptions{Primary: []string{"name"}, UpdateTime: false})
	require.NoError(t, err)

	p := &Plugin{meta: plugins.Metadata{}, Now: time.Now}
	require.NoError(t, p.PostInit(ctxA, h))

	resA, err := p.Process(ctxA, snoozetypes.Record{Source: "syslog"})
	require.NoError(t, err)
	require.NotEqual(t, plugins.ActionContinue, resA.Action, "rule should fire for acme")

	// beta has no rules — must pass through.
	require.NoError(t, p.Reload(ctxB))
	resB, err := p.Process(ctxB, snoozetypes.Record{Source: "syslog"})
	require.NoError(t, err)
	require.Equal(t, plugins.ActionContinue, resB.Action)

	// Reloading beta's (empty) partition must not evict acme's rules.
	resA2, err := p.Process(ctxA, snoozetypes.Record{Source: "syslog"})
	require.NoError(t, err)
	require.NotEqual(t, plugins.ActionContinue, resA2.Action, "acme rules must survive beta reload")
}

// TestSnoozeHitsCounter covers the synchronous hit-counter bump performed on
// each match. The Python version uses an AsyncIncrement; we trade off
// throughput for simplicity (see plugin.go's package doc).
func TestSnoozeHitsCounter(t *testing.T) {
	t.Parallel()
	h := newStubHost(t)
	uid := writeRule(t, h, db.Document{
		"name":      "Filter 1",
		"condition": []any{"=", "a", "1"},
	})
	p := newPlugin(t, h, nil)

	ctx := auth.WithTenant(context.Background(), snoozetypes.DefaultTenant)
	rec := snoozetypes.Record{Extra: map[string]any{"a": "1"}}
	for i := 0; i < 3; i++ {
		_, err := p.Process(ctx, rec)
		require.NoError(t, err)
	}

	got, err := h.driver.GetOne(ctx, "snooze", db.Document{"uid": uid})
	require.NoError(t, err)
	hits, ok := toInt64(got["hits"])
	require.True(t, ok, "hits field missing or non-numeric: %#v", got["hits"])
	require.EqualValues(t, 3, hits)
}

// TestSnoozeAlertSnoozedCounter verifies that Process enqueues an
// alert_snoozed increment (metric="alert_snoozed", dim="name",
// key=<filter name>, bucket=hour-truncated epoch) via RecordStat whenever a
// filter matches.
func TestSnoozeAlertSnoozedCounter(t *testing.T) {
	t.Parallel()

	// Build a host with metrics enabled and a capturing async writer.
	h := newStubHost(t)
	// config.Default() already has MetricsEnabled:true via schema.DefaultGeneral.
	capDrv, calls := newCaptureDrv()
	h.asyncWriter = asyncwriter.New(capDrv, time.Hour,
		asyncwriter.NewMockClock(time.Unix(0, 0)),
		asyncwriter.WithUpsert(true))

	writeRule(t, h, db.Document{
		"name":      "Maintenance",
		"condition": []any{"=", "host", "h1"},
	})
	p := newPlugin(t, h, nil)

	ctx := auth.WithTenant(context.Background(), snoozetypes.DefaultTenant)
	// rec.DateEpoch=1780302245 → hour bucket 1780300800.
	rec := snoozetypes.Record{Host: "h1", DateEpoch: 1780302245}
	_, err := p.Process(ctx, rec)
	require.NoError(t, err)

	// Flush queued increments to the capture driver.
	require.NoError(t, h.asyncWriter.Flush(context.Background()))

	require.Len(t, *calls, 1, "expected exactly one alert_snoozed increment")
	c := (*calls)[0]
	require.Equal(t, "alert_snoozed", c.metric)
	require.Equal(t, "name", c.dim)
	require.Equal(t, "Maintenance", c.key)
	require.Equal(t, int64(1780300800), c.bucket)
	require.Equal(t, int64(1), c.delta)
}

// TestSnoozeListProjection seeds a rule with a future `until` and fetches it
// through the generic CRUD list/get-one handlers wired to the snooze plugin.
// Because the plugin implements plugins.DocTransformer, both read paths must
// project window_status and remaining_seconds onto the document. This is the
// integration guard that ties ProjectDoc to the HTTP read surface.
func TestSnoozeListProjection(t *testing.T) {
	t.Parallel()
	h := newStubHost(t)
	writeRule(t, h, db.Document{
		"name":      "Quiet until morning",
		"condition": []any{"=", "host", "h1"},
		"time_constraints": map[string]any{
			"datetime": []any{
				map[string]any{
					"from":  fixedNow.Add(-time.Hour).Format(time.RFC3339),
					"until": fixedNow.Add(time.Hour).Format(time.RFC3339),
				},
			},
		},
	})

	// Plugin with a deterministic clock pinned to fixedNow.
	p := newPlugin(t, h, func() time.Time { return fixedNow })

	// Mount the generic CRUD surface against the plugin and issue read requests
	// with admin claims (AuthorizeCRUD would 401 otherwise).
	r := chi.NewRouter()
	plugins.MountCRUD(r, h, p)

	withClaims := func(req *http.Request) *http.Request {
		ctx := auth.WithClaims(req.Context(), snoozetypes.Claims{
			Subject:     "test",
			Method:      "local",
			Roles:       []string{"admin"},
			Permissions: []string{"rw_all"},
		})
		ctx = auth.WithTenant(ctx, snoozetypes.DefaultTenant)
		return req.WithContext(ctx)
	}

	// --- GET /api/v1/snooze (list) ---
	listReq := withClaims(httptest.NewRequest(http.MethodGet, "/api/v1/snooze", nil))
	listRec := httptest.NewRecorder()
	r.ServeHTTP(listRec, listReq)
	require.Equal(t, http.StatusOK, listRec.Code, listRec.Body.String())

	var list struct {
		Data []db.Document `json:"data"`
	}
	require.NoError(t, json.Unmarshal(listRec.Body.Bytes(), &list))
	require.Len(t, list.Data, 1)
	got := list.Data[0]
	require.Equal(t, "active", got["window_status"])
	// JSON numbers decode to float64; the projection produced ~3600s.
	remaining, ok := got["remaining_seconds"].(float64)
	require.True(t, ok, "remaining_seconds missing or non-numeric: %#v", got["remaining_seconds"])
	require.InDelta(t, 3600, remaining, 5)

	uid, _ := got["uid"].(string)
	require.NotEmpty(t, uid)

	// --- GET /api/v1/snooze/{uid} (get-one) ---
	oneReq := withClaims(httptest.NewRequest(http.MethodGet, "/api/v1/snooze/"+uid, nil))
	oneRec := httptest.NewRecorder()
	r.ServeHTTP(oneRec, oneReq)
	require.Equal(t, http.StatusOK, oneRec.Code, oneRec.Body.String())

	var one db.Document
	require.NoError(t, json.Unmarshal(oneRec.Body.Bytes(), &one))
	require.Equal(t, "active", one["window_status"])
	require.Contains(t, one, "remaining_seconds")
}

// TestSnoozeCloseTransition_DiscardRuleBypassed covers production bug #1: a
// close against an EXISTING aggregate (duplicates>=2) must reach the
// pipeline unharmed even when a discard rule matches — the aggregaterule
// close write must never be dropped, or the alert is wedged open forever.
func TestSnoozeCloseTransition_DiscardRuleBypassed(t *testing.T) {
	t.Parallel()
	h := newStubHost(t)
	uid := writeRule(t, h, db.Document{
		"name":      "Filter 1",
		"condition": []any{"=", "host", "h1"},
		"discard":   true,
	})
	p := newPlugin(t, h, nil)

	ctx := auth.WithTenant(context.Background(), snoozetypes.DefaultTenant)
	rec := snoozetypes.Record{
		Host:  "h1",
		State: "close",
		Extra: map[string]any{"duplicates": int64(2)},
	}
	res, err := p.Process(ctx, rec)
	require.NoError(t, err)
	require.Equal(t, plugins.ActionContinue, res.Action)
	require.Nil(t, res.Record.Extra["snoozed"], "close pass-through must not stamp a snooze attribution")

	got, err := h.driver.GetOne(ctx, "snooze", db.Document{"uid": uid})
	require.NoError(t, err)
	hits, _ := toInt64(got["hits"])
	require.Zero(t, hits, "close pass-through must not bump the rule's hit counter")
}

// TestSnoozeCloseTransition_TagRuleBypassed is the same close-transition
// pass-through, but against a non-discard (tag) rule: it must still return
// ActionContinue with no `snoozed` tag, since the close never reaches the
// rule loop at all.
func TestSnoozeCloseTransition_TagRuleBypassed(t *testing.T) {
	t.Parallel()
	h := newStubHost(t)
	writeRule(t, h, db.Document{
		"name":      "Filter 1",
		"condition": []any{"=", "host", "h1"},
	})
	p := newPlugin(t, h, nil)

	ctx := auth.WithTenant(context.Background(), snoozetypes.DefaultTenant)
	rec := snoozetypes.Record{
		Host:  "h1",
		State: "close",
		Extra: map[string]any{"duplicates": int64(2)},
	}
	res, err := p.Process(ctx, rec)
	require.NoError(t, err)
	require.Equal(t, plugins.ActionContinue, res.Action)
	require.Nil(t, res.Record.Extra["snoozed"])
}

// TestSnoozeCloseTransition_FirstOccurrenceStillFiltered covers the
// first-occurrence close (duplicates absent, or stamped 1 when no existing
// aggregate matched): the state gate must NOT engage, so a fully-discarded
// alert's recovery event cannot leak a phantom closed row — the discard
// rule still applies.
func TestSnoozeCloseTransition_FirstOccurrenceStillFiltered(t *testing.T) {
	t.Parallel()

	t.Run("duplicates absent", func(t *testing.T) {
		h := newStubHost(t)
		writeRule(t, h, db.Document{
			"name":      "Filter 1",
			"condition": []any{"=", "host", "h1"},
			"discard":   true,
		})
		p := newPlugin(t, h, nil)

		ctx := auth.WithTenant(context.Background(), snoozetypes.DefaultTenant)
		rec := snoozetypes.Record{Host: "h1", State: "close"}
		res, err := p.Process(ctx, rec)
		require.NoError(t, err)
		require.Equal(t, plugins.ActionAbort, res.Action)
	})

	t.Run("duplicates=1", func(t *testing.T) {
		h := newStubHost(t)
		writeRule(t, h, db.Document{
			"name":      "Filter 1",
			"condition": []any{"=", "host", "h1"},
			"discard":   true,
		})
		p := newPlugin(t, h, nil)

		ctx := auth.WithTenant(context.Background(), snoozetypes.DefaultTenant)
		rec := snoozetypes.Record{
			Host:  "h1",
			State: "close",
			Extra: map[string]any{"duplicates": int64(1)},
		}
		res, err := p.Process(ctx, rec)
		require.NoError(t, err)
		require.Equal(t, plugins.ActionAbort, res.Action)
	})
}

// TestSnoozeCloseTransition_RequiresCloseState verifies the state gate is
// required: a non-close record with duplicates>=2 still runs the rules
// normally (the pass-through is specific to state=="close").
func TestSnoozeCloseTransition_RequiresCloseState(t *testing.T) {
	t.Parallel()
	h := newStubHost(t)
	writeRule(t, h, db.Document{
		"name":      "Filter 1",
		"condition": []any{"=", "host", "h1"},
		"discard":   true,
	})
	p := newPlugin(t, h, nil)

	ctx := auth.WithTenant(context.Background(), snoozetypes.DefaultTenant)
	rec := snoozetypes.Record{
		Host:  "h1",
		State: "open",
		Extra: map[string]any{"duplicates": int64(5)},
	}
	res, err := p.Process(ctx, rec)
	require.NoError(t, err)
	require.Equal(t, plugins.ActionAbort, res.Action)
}

// TestSnoozeBypassSeverity_RuntimeSettingsOverridesFileConfig covers
// production bug #2: the severity-bypass list must be read from the
// DB-backed runtime settings store (what the settings UI writes to) rather
// than only the boot-time file config, so operator edits take effect live.
func TestSnoozeBypassSeverity_RuntimeSettingsOverridesFileConfig(t *testing.T) {
	t.Parallel()
	h := newStubHost(t)
	// File config has no bypass severities configured.
	h.cfg.General.SnoozeBySeverities = nil

	ctx := auth.WithTenant(context.Background(), snoozetypes.DefaultTenant)
	_, err := h.driver.Write(ctx, "settings", []db.Document{
		{"name": "snooze_bypass_severities", "value": []string{"ok"}},
	}, db.WriteOptions{Primary: []string{"name"}, UpdateTime: false})
	require.NoError(t, err)

	h.runtimeSet = config.NewRuntimeSettings(h.driver, h.cfg, time.Minute)

	// Catch-all rule: no condition → matches every record.
	writeRule(t, h, db.Document{"name": "catch-all"})
	p := newPlugin(t, h, nil)

	// severity "OK" → bypassed via the runtime store, even though the file
	// config's SnoozeBySeverities is empty.
	res, err := p.Process(ctx, snoozetypes.Record{Severity: "OK"})
	require.NoError(t, err)
	require.Equal(t, plugins.ActionContinue, res.Action)
	require.Nil(t, res.Record.Extra["snoozed"])

	// severity "critical" → not in the bypass list; catch-all must fire.
	res2, err := p.Process(ctx, snoozetypes.Record{Severity: "critical"})
	require.NoError(t, err)
	require.NotEqual(t, plugins.ActionContinue, res2.Action)
}

// TestSnoozeFilter_MatchesProcess pins plugins.Filter to the same verdict
// Process gives. The pipeline calls Filter when an earlier processor cut the
// run short with an abort-and-persist verdict (aggregaterule's throttle and
// anti-flapping holds), and the suppression decision must not differ from the
// one the ordinary pass would have made.
func TestSnoozeFilter_MatchesProcess(t *testing.T) {
	t.Parallel()
	h := newStubHost(t)
	writeRule(t, h, db.Document{
		"name":      "WAF processes",
		"condition": []any{"=", "process", "WAF alerts"},
	})
	writeRule(t, h, db.Document{
		"name":      "Warnings",
		"condition": []any{"=", "severity", "warning"},
		"discard":   true,
	})
	p := newPlugin(t, h, nil)
	ctx := auth.WithTenant(context.Background(), snoozetypes.DefaultTenant)

	// A throttled duplicate of the live WAF alert: the older, narrower rule
	// wins, so the record is kept and attributed rather than discarded.
	rec := snoozetypes.Record{Process: "WAF alerts", Severity: "warning", State: "open"}
	res, err := p.Filter(ctx, rec)
	require.NoError(t, err)
	require.Equal(t, plugins.ActionAbortWrite, res.Action)
	require.Equal(t, "WAF processes", res.Record.Extra["snoozed"])

	// And a plain warning that only the discard rule covers is dropped.
	res, err = p.Filter(ctx, snoozetypes.Record{Severity: "warning", State: "open"})
	require.NoError(t, err)
	require.Equal(t, plugins.ActionAbort, res.Action)
}

// TestSnoozeAfterDelete_ClearsStaleAttribution is the regression test for the
// residue a deleted filter used to leave behind: `snoozed` is an attribution
// BY NAME and the alerts list treats a record carrying it as silenced, so
// deleting a broad filter left every record it had stamped hidden behind a
// filter that no longer existed.
func TestSnoozeAfterDelete_ClearsStaleAttribution(t *testing.T) {
	t.Parallel()
	h := newStubHost(t)
	uid := writeRule(t, h, db.Document{
		"name":      "all",
		"condition": []any{"=", "severity", "warning"},
	})
	p := newPlugin(t, h, nil)
	ctx := auth.WithTenant(context.Background(), snoozetypes.DefaultTenant)

	written, err := h.driver.Write(ctx, recordCollection, []db.Document{
		{"name": "rec-stamped", "snoozed": "all", "severity": "warning"},
		{"name": "rec-other", "snoozed": "Warnings", "severity": "warning"},
		{"name": "rec-clean", "severity": "warning"},
	}, db.WriteOptions{UpdateTime: true})
	require.NoError(t, err)
	require.Len(t, written.Added, 3)
	stampedUID, otherUID := written.Added[0], written.Added[1]

	// The CRUD delete handler order: GuardDelete, then the DB delete, then
	// AfterDelete. GuardDelete is what captures the name.
	require.NoError(t, p.GuardDelete(ctx, []string{uid}))
	_, err = h.driver.Delete(ctx, collectionName, condition.Equals("uid", uid), false)
	require.NoError(t, err)
	require.NoError(t, p.AfterDelete(ctx, []string{uid}))

	stamped, err := h.driver.GetOne(ctx, recordCollection, db.Document{"uid": stampedUID})
	require.NoError(t, err)
	require.NotContains(t, stamped, "snoozed",
		"a record attributed to the deleted filter must not stay silenced")

	other, err := h.driver.GetOne(ctx, recordCollection, db.Document{"uid": otherUID})
	require.NoError(t, err)
	require.Equal(t, "Warnings", other["snoozed"],
		"records attributed to a surviving filter must be left alone")
}

// TestSnoozeAfterDelete_WithoutGuardIsANoop: AfterDelete has no name to work
// from if GuardDelete never ran (or the uid was unknown), and must not touch
// the record collection on a guess.
func TestSnoozeAfterDelete_WithoutGuardIsANoop(t *testing.T) {
	t.Parallel()
	h := newStubHost(t)
	p := newPlugin(t, h, nil)
	ctx := auth.WithTenant(context.Background(), snoozetypes.DefaultTenant)

	written, err := h.driver.Write(ctx, recordCollection, []db.Document{
		{"name": "rec-keep", "snoozed": "all"},
	}, db.WriteOptions{UpdateTime: true})
	require.NoError(t, err)
	require.Len(t, written.Added, 1)

	require.NoError(t, p.AfterDelete(ctx, []string{"never-guarded"}))

	kept, err := h.driver.GetOne(ctx, recordCollection, db.Document{"uid": written.Added[0]})
	require.NoError(t, err)
	require.Equal(t, "all", kept["snoozed"])
}
