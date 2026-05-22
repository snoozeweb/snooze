// Tests mirroring tests/plugins/core/test_rule.py:
//
//   - TestRule_Match: condition matches → "rules" tag is appended.
//   - TestRule_Modify: modifications mutate the record (condition bypassed).
//   - TestRule_Process: full pipeline with a 3-level rule tree.
//
// The DB is a per-test SQLite file under t.TempDir() — never `:memory:` or
// the shared-cache memory URI, which doesn't isolate parallel tests (see the
// note in internal/db/sqlite/driver_test.go).
package rule

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/config"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/db/sqlite"
	"github.com/snoozeweb/snooze/internal/modification"
	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/internal/telemetry"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// testHost is a minimal plugins.Host backed by a sqlite driver.
type testHost struct {
	driver db.Driver
	logger *slog.Logger
	cfg    *config.Config
	metr   *telemetry.Registry
	tracer trace.Tracer
	plugs  map[string]plugins.Plugin
}

func newTestHost(t *testing.T) *testHost {
	t.Helper()
	path := filepath.Join(t.TempDir(), "snooze.db")
	d, err := sqlite.New(context.Background(), sqlite.Config{Path: path})
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	return &testHost{
		driver: d,
		logger: slog.Default(),
		cfg:    config.Default(),
		metr:   telemetry.NewRegistry(nil),
		tracer: otel.Tracer("rule-test"),
		plugs:  map[string]plugins.Plugin{},
	}
}

func (h *testHost) DB() db.Driver                { return h.driver }
func (h *testHost) Bus() plugins.Bus             { return nil }
func (h *testHost) Logger() *slog.Logger         { return h.logger }
func (h *testHost) Tracer() trace.Tracer         { return h.tracer }
func (h *testHost) Metrics() *telemetry.Registry { return h.metr }
func (h *testHost) Config() *config.Config       { return h.cfg }
func (h *testHost) Plugin(name string) plugins.Plugin {
	return h.plugs[name]
}

// makeRecord returns a Record whose Extra carries the Python-test fields.
func makeRecord(extra map[string]any) snoozetypes.Record {
	return snoozetypes.Record{Extra: extra}
}

func TestRule_Match(t *testing.T) {
	// Mirrors TestRule::test_match in tests/plugins/core/test_rule.py.
	t.Parallel()

	view := map[string]any{"a": "1", "b": "2"}
	cond, err := condition.FromList([]any{"=", "a", "1"})
	require.NoError(t, err)

	require.True(t, condition.Match(view, cond))
	appendRuleTag(view, "Rule1")

	require.Equal(t, map[string]any{
		"a":     "1",
		"b":     "2",
		"rules": []any{"Rule1"},
	}, view)
}

func TestRule_Modify(t *testing.T) {
	// Mirrors TestRule::test_modify. Condition is bypassed; modifications run
	// directly against the view.
	t.Parallel()

	view := map[string]any{"a": "1", "b": "2"}
	mods := []modification.Modification{
		{Op: modification.OpSet, Args: []any{"a", "2"}},
		{Op: modification.OpSet, Args: []any{"c", "3"}},
	}
	applyModifications(view, mods)

	require.Equal(t, map[string]any{
		"a": "2",
		"b": "2",
		"c": "3",
	}, view)
}

func TestRule_Process(t *testing.T) {
	// Mirrors TestRulesPlugin::test_process: three nested rules write to
	// fields that re-trigger the next level.
	t.Parallel()

	host := newTestHost(t)
	ctx := context.Background()

	// Insert top-level rule.
	res, err := host.DB().Write(ctx, "rule", []db.Document{{
		"name":          "Rule1",
		"condition":     []any{"=", "a", "1"},
		"modifications": []any{[]any{"SET", "c", "1"}},
	}}, db.WriteOptions{})
	require.NoError(t, err)
	require.Len(t, res.Added, 1)
	uid1 := res.Added[0]

	// Insert child rule whose `parents` array contains uid1.
	res, err = host.DB().Write(ctx, "rule", []db.Document{{
		"name":          "SubRule1",
		"condition":     []any{"=", "c", "1"},
		"modifications": []any{[]any{"SET", "c", "4"}, []any{"SET", "b", "4"}},
		"parents":       []any{uid1},
	}}, db.WriteOptions{})
	require.NoError(t, err)
	require.Len(t, res.Added, 1)
	uid2 := res.Added[0]

	// Insert grand-child rule.
	_, err = host.DB().Write(ctx, "rule", []db.Document{{
		"name":          "SubSubRule1",
		"condition":     []any{"=", "c", "4"},
		"modifications": []any{[]any{"SET", "c", "5"}},
		"parents":       []any{uid2},
	}}, db.WriteOptions{})
	require.NoError(t, err)

	// Build the plugin via the registered factory so it exercises the public
	// path (metadata parsing, PostInit -> Reload).
	p := &Plugin{}
	require.NoError(t, p.PostInit(ctx, host))

	out, err := p.Process(ctx, makeRecord(map[string]any{"a": "1", "b": "2"}))
	require.NoError(t, err)
	require.Equal(t, plugins.ActionContinue, out.Action)
	require.NotNil(t, out.Record.Extra)
	require.Equal(t, "1", out.Record.Extra["a"])
	require.Equal(t, "4", out.Record.Extra["b"])
	require.Equal(t, "5", out.Record.Extra["c"])
	require.Equal(t, []any{"Rule1", "SubRule1", "SubSubRule1"}, out.Record.Extra["rules"])
}

// stubKV satisfies the rule package's kvGetter duck-type with a literal map
// so the test exercises the cached-lookup path without spinning the real kv
// plugin.
type stubKV struct{ data map[string]map[string]any }

func (s *stubKV) Name() string                     { return "kv" }
func (s *stubKV) Metadata() plugins.Metadata       { return plugins.Metadata{Name: "kv"} }
func (s *stubKV) PostInit(context.Context, plugins.Host) error { return nil }
func (s *stubKV) Reload(context.Context) error     { return nil }
func (s *stubKV) Get(dict, key string) (any, bool) {
	d, ok := s.data[dict]
	if !ok {
		return nil, false
	}
	v, ok := d[key]
	return v, ok
}

func TestRule_KVSet_WireShape(t *testing.T) {
	// Wire shape from Python: ["KV_SET", dict, key, out_field].
	// Semantics: record[out_field] = kv[dict][record[key]].
	t.Parallel()

	host := newTestHost(t)
	ctx := context.Background()
	host.plugs["kv"] = &stubKV{data: map[string]map[string]any{
		"host_owner": {"web01": "alice", "db01": "bob"},
	}}

	_, err := host.DB().Write(ctx, "rule", []db.Document{{
		"name":          "OwnerLookup",
		"condition":     []any{"=", "host", "web01"},
		"modifications": []any{[]any{"KV_SET", "host_owner", "host", "owner"}},
	}}, db.WriteOptions{})
	require.NoError(t, err)

	p := &Plugin{}
	require.NoError(t, p.PostInit(ctx, host))

	out, err := p.Process(ctx, makeRecord(map[string]any{"host": "web01"}))
	require.NoError(t, err)
	require.Equal(t, "alice", out.Record.Extra["owner"])
}

func TestRule_KVSet_DBFallback(t *testing.T) {
	// When the kv plugin handle isn't registered, applyKVSets falls back to
	// a direct lookup on the "kv" collection. The on-disk shape is the
	// Python-era {dict, key, value} record set.
	t.Parallel()

	host := newTestHost(t)
	ctx := context.Background()

	_, err := host.DB().Write(ctx, "kv", []db.Document{
		{"dict": "host_owner", "key": "web01", "value": "alice"},
	}, db.WriteOptions{})
	require.NoError(t, err)

	_, err = host.DB().Write(ctx, "rule", []db.Document{{
		"name":          "OwnerLookup",
		"condition":     []any{"=", "host", "web01"},
		"modifications": []any{[]any{"KV_SET", "host_owner", "host", "owner"}},
	}}, db.WriteOptions{})
	require.NoError(t, err)

	p := &Plugin{}
	require.NoError(t, p.PostInit(ctx, host))

	out, err := p.Process(ctx, makeRecord(map[string]any{"host": "web01"}))
	require.NoError(t, err)
	require.Equal(t, "alice", out.Record.Extra["owner"])
}

func TestRule_KVSet_MissDoesNotMutate(t *testing.T) {
	// A missing record[key] or a missing kv[dict][record_key] entry must
	// leave the record untouched (matches Python's KeyError swallow).
	t.Parallel()

	host := newTestHost(t)
	ctx := context.Background()
	host.plugs["kv"] = &stubKV{data: map[string]map[string]any{
		"host_owner": {"web01": "alice"},
	}}

	_, err := host.DB().Write(ctx, "rule", []db.Document{{
		"name":          "OwnerLookup",
		"condition":     []any{"=", "host", "unknown"},
		"modifications": []any{[]any{"KV_SET", "host_owner", "host", "owner"}},
	}}, db.WriteOptions{})
	require.NoError(t, err)

	p := &Plugin{}
	require.NoError(t, p.PostInit(ctx, host))

	out, err := p.Process(ctx, makeRecord(map[string]any{"host": "unknown"}))
	require.NoError(t, err)
	_, has := out.Record.Extra["owner"]
	require.False(t, has)
}

func TestRule_NameAndMetadata(t *testing.T) {
	// Sanity check the basic Plugin contract.
	t.Parallel()

	meta, err := plugins.ParseMetadata(metaYAML)
	require.NoError(t, err)
	require.Equal(t, "Rule", meta.Name)
	require.True(t, meta.AutoReload)
	require.True(t, meta.Tree)

	p := &Plugin{meta: meta}
	require.Equal(t, "rule", p.Name())
	require.Equal(t, meta, p.Metadata())
}
