package reject

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/config"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/internal/syncer"
	"github.com/snoozeweb/snooze/internal/telemetry"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// fakeHost is a minimal plugins.Host used in tests. It holds no real DB; the
// reject plugin's in-process cache is loaded by directly setting p.rules in
// test helpers, avoiding SQLite.
type fakeHost struct {
	logger *slog.Logger
	cfg    *config.Config
	tracer trace.Tracer
	metr   *telemetry.Registry
}

func newFakeHost() *fakeHost {
	return &fakeHost{
		logger: slog.Default(),
		cfg:    config.Default(),
		tracer: otel.Tracer("reject-plugin-test"),
		metr:   telemetry.NewRegistry(nil),
	}
}

func (h *fakeHost) DB() db.Driver                { return nil }
func (h *fakeHost) Bus() plugins.Bus             { return nil }
func (h *fakeHost) Logger() *slog.Logger         { return h.logger }
func (h *fakeHost) Tracer() trace.Tracer         { return h.tracer }
func (h *fakeHost) Metrics() *telemetry.Registry { return h.metr }
func (h *fakeHost) Config() *config.Config       { return h.cfg }
func (h *fakeHost) Plugin(string) plugins.Plugin { return nil }

// noopDrv is a db.Driver whose Search method returns the rules injected via
// searchDocs, allowing in-memory Reload testing without SQLite.
type noopDrv struct {
	searchDocs []db.Document
}

func (d *noopDrv) Search(_ context.Context, _ string, _ condition.Cond, _ db.Page) ([]db.Document, int, error) {
	return d.searchDocs, len(d.searchDocs), nil
}
func (d *noopDrv) GetOne(context.Context, string, db.Document) (db.Document, error) {
	return nil, nil
}
func (d *noopDrv) Write(context.Context, string, []db.Document, db.WriteOptions) (db.WriteResult, error) {
	return db.WriteResult{}, nil
}
func (d *noopDrv) ReplaceOne(context.Context, string, db.Document, db.Document, bool) (int, error) {
	return 0, nil
}
func (d *noopDrv) UpdateOne(context.Context, string, string, db.Document, bool) error { return nil }
func (d *noopDrv) Delete(context.Context, string, condition.Cond, bool) (int, error)  { return 0, nil }
func (d *noopDrv) Convert(context.Context, condition.Cond, []string) (db.DriverQuery, error) {
	return nil, nil
}
func (d *noopDrv) IncMany(context.Context, string, string, condition.Cond, int64) (int, error) {
	return 0, nil
}
func (d *noopDrv) SetFields(context.Context, string, db.Document, condition.Cond) (int, error) {
	return 0, nil
}
func (d *noopDrv) UnsetFields(context.Context, string, []string, condition.Cond) (int, error) {
	return 0, nil
}
func (d *noopDrv) AppendList(context.Context, string, map[string][]any, condition.Cond) (int, error) {
	return 0, nil
}
func (d *noopDrv) PrependList(context.Context, string, map[string][]any, condition.Cond) (int, error) {
	return 0, nil
}
func (d *noopDrv) RemoveList(context.Context, string, map[string][]any, condition.Cond) (int, error) {
	return 0, nil
}
func (d *noopDrv) BulkIncrement(context.Context, string, []db.IncrementOp, bool) error { return nil }
func (d *noopDrv) CreateIndex(context.Context, string, []string) error                 { return nil }
func (d *noopDrv) ListCollections(context.Context) ([]string, error)                   { return nil, nil }
func (d *noopDrv) Drop(context.Context, string) error                                  { return nil }
func (d *noopDrv) Backup(context.Context, string, []string) error                      { return nil }
func (d *noopDrv) CleanupTimeout(context.Context, string) (int, error)                 { return 0, nil }
func (d *noopDrv) CleanupComments(context.Context) (int, error)                        { return 0, nil }
func (d *noopDrv) CleanupOrphans(context.Context, string) (int, error)                 { return 0, nil }
func (d *noopDrv) CleanupAuditLogs(context.Context, time.Duration) (int, error)        { return 0, nil }
func (d *noopDrv) CleanupSnooze(context.Context) (int, error)                          { return 0, nil }
func (d *noopDrv) CleanupNotification(context.Context) (int, error)                    { return 0, nil }
func (d *noopDrv) ComputeStats(context.Context, string, time.Time, time.Time, string) ([]db.StatsBucket, error) {
	return nil, nil
}
func (d *noopDrv) Watcher() syncer.Bus { return nil }
func (d *noopDrv) Close() error        { return nil }

// reloadHost is a Host variant that returns a noopDrv, letting us control what
// Reload loads.
type reloadHost struct {
	fakeHost
	drv *noopDrv
}

func newReloadHost(docs []db.Document) *reloadHost {
	h := &reloadHost{
		fakeHost: *newFakeHost(),
		drv:      &noopDrv{searchDocs: docs},
	}
	return h
}

func (h *reloadHost) DB() db.Driver { return h.drv }

// newPluginWithRules constructs a Plugin with the given rules pre-loaded into
// the default tenant's cache, bypassing DB interaction.
func newPluginWithRules(t *testing.T, rules []rejectRule) *Plugin {
	t.Helper()
	p := &Plugin{meta: plugins.Metadata{}}
	p.rules = map[string][]rejectRule{
		snoozetypes.DefaultTenant: rules,
	}
	p.host = newFakeHost()
	return p
}

func defaultCtx() context.Context {
	return auth.WithTenant(context.Background(), snoozetypes.DefaultTenant)
}

// TestRejectPlugin_NoRules_Continue — empty cache → ActionContinue.
func TestRejectPlugin_NoRules_Continue(t *testing.T) {
	t.Parallel()
	p := newPluginWithRules(t, nil)

	res, err := p.Process(defaultCtx(), snoozetypes.Record{Host: "myhost"})
	require.NoError(t, err)
	require.Equal(t, plugins.ActionContinue, res.Action)
	require.Empty(t, res.Record.Extra["reject_policy"])
}

// TestRejectPlugin_MatchedRule_Aborts — one enabled rule whose condition
// matches → ActionAbort, rec.Extra["reject_policy"] set to rule name.
func TestRejectPlugin_MatchedRule_Aborts(t *testing.T) {
	t.Parallel()
	cond, err := condition.FromList([]any{"=", "source", "blacklisted"})
	require.NoError(t, err)

	p := newPluginWithRules(t, []rejectRule{
		{Name: "blacklist", Enabled: true, Cond: cond},
	})

	rec := snoozetypes.Record{Source: "blacklisted"}
	res, err := p.Process(defaultCtx(), rec)
	require.NoError(t, err)
	require.Equal(t, plugins.ActionAbort, res.Action)
	require.Equal(t, "blacklist", res.Record.Extra["reject_policy"])
}

// TestRejectPlugin_DisabledRule_Continue — matching but enabled=false rule → ActionContinue.
func TestRejectPlugin_DisabledRule_Continue(t *testing.T) {
	t.Parallel()
	cond, err := condition.FromList([]any{"=", "source", "blacklisted"})
	require.NoError(t, err)

	p := newPluginWithRules(t, []rejectRule{
		{Name: "disabled-rule", Enabled: false, Cond: cond},
	})

	rec := snoozetypes.Record{Source: "blacklisted"}
	res, err := p.Process(defaultCtx(), rec)
	require.NoError(t, err)
	require.Equal(t, plugins.ActionContinue, res.Action)
	require.Empty(t, res.Record.Extra["reject_policy"])
}

// TestRejectPlugin_MultipleRules_FirstMatch — two rules, first matches →
// Abort on first rule name.
func TestRejectPlugin_MultipleRules_FirstMatch(t *testing.T) {
	t.Parallel()
	cond1, err := condition.FromList([]any{"=", "source", "src1"})
	require.NoError(t, err)
	cond2, err := condition.FromList([]any{"=", "source", "src1"}) // same condition
	require.NoError(t, err)

	p := newPluginWithRules(t, []rejectRule{
		{Name: "first-rule", Enabled: true, Cond: cond1},
		{Name: "second-rule", Enabled: true, Cond: cond2},
	})

	rec := snoozetypes.Record{Source: "src1"}
	res, err := p.Process(defaultCtx(), rec)
	require.NoError(t, err)
	require.Equal(t, plugins.ActionAbort, res.Action)
	require.Equal(t, "first-rule", res.Record.Extra["reject_policy"])
}

// TestRejectPlugin_Reload_UpdatesCache — load rules, reload with new set,
// confirm cache updated.
func TestRejectPlugin_Reload_UpdatesCache(t *testing.T) {
	t.Parallel()

	initialDocs := []db.Document{
		{"name": "rule-a", "enabled": true, "condition": []any{"=", "host", "h1"}},
	}
	h := newReloadHost(initialDocs)
	p := &Plugin{meta: plugins.Metadata{}}
	ctx := defaultCtx()
	require.NoError(t, p.PostInit(ctx, h))
	require.Len(t, p.cachedRules(snoozetypes.DefaultTenant), 1)

	// Update the driver to return a different set.
	h.drv.searchDocs = []db.Document{
		{"name": "rule-a", "enabled": true, "condition": []any{"=", "host", "h1"}},
		{"name": "rule-b", "enabled": true, "condition": []any{"=", "host", "h2"}},
	}
	require.NoError(t, p.Reload(ctx))
	require.Len(t, p.cachedRules(snoozetypes.DefaultTenant), 2)
}

// TestRejectPlugin_Metadata — p.Name() == "reject", Metadata().Name not empty.
func TestRejectPlugin_Metadata(t *testing.T) {
	t.Parallel()
	h := newFakeHost()
	p := &Plugin{meta: plugins.Metadata{}}
	// PostInit with nil DB: cache cleared, no error.
	require.NoError(t, p.PostInit(defaultCtx(), h))
	require.Equal(t, "reject", p.Name())
}

// TestRejectPlugin_NilExtraInitialized — Process initializes rec.Extra when nil.
func TestRejectPlugin_NilExtraInitialized(t *testing.T) {
	t.Parallel()
	cond, err := condition.FromList([]any{"=", "host", "target"})
	require.NoError(t, err)

	p := newPluginWithRules(t, []rejectRule{
		{Name: "gate", Enabled: true, Cond: cond},
	})

	// Record with nil Extra.
	rec := snoozetypes.Record{Host: "target"}
	require.Nil(t, rec.Extra)

	res, err := p.Process(defaultCtx(), rec)
	require.NoError(t, err)
	require.Equal(t, plugins.ActionAbort, res.Action)
	require.Equal(t, "gate", res.Record.Extra["reject_policy"])
}
