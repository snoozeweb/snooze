package plugins

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/config"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/syncer"
	"github.com/snoozeweb/snooze/internal/telemetry"
)

// memDB is a tiny, race-safe in-memory implementation of db.Driver used only
// to drive the CRUD HTTP handlers in tests. It is intentionally not optimised
// and does not implement maintenance, bulk-incr, etc. (those return errUnsup).
type memDB struct {
	mu   sync.Mutex
	data map[string][]db.Document // collection -> docs (each carries a "uid")
}

var errUnsup = errors.New("memDB: unsupported in test")

func newMemDB() *memDB { return &memDB{data: map[string][]db.Document{}} }

func (m *memDB) docs(col string) []db.Document { return m.data[col] }

func (m *memDB) Search(_ context.Context, col string, cond condition.Cond, page db.Page) ([]db.Document, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	all := m.data[col]
	out := make([]db.Document, 0, len(all))
	for _, d := range all {
		if cond.IsZero() || condition.Match(d, cond) {
			out = append(out, d)
		}
	}
	total := len(out)
	if page.PerPage > 0 {
		start := 0
		if page.PageNb > 1 {
			start = (page.PageNb - 1) * page.PerPage
		}
		if start >= len(out) {
			out = nil
		} else {
			end := start + page.PerPage
			if end > len(out) {
				end = len(out)
			}
			out = out[start:end]
		}
	}
	return out, total, nil
}

func (m *memDB) GetOne(_ context.Context, col string, match db.Document) (db.Document, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	uid, _ := match["uid"].(string)
	for _, d := range m.data[col] {
		if d["uid"] == uid {
			return d, nil
		}
	}
	return nil, errors.New("not found")
}

func (m *memDB) Convert(context.Context, condition.Cond, []string) (db.DriverQuery, error) {
	return nil, errUnsup
}

func (m *memDB) Write(_ context.Context, col string, docs []db.Document, _ db.WriteOptions) (db.WriteResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var added []string
	for _, d := range docs {
		uid, _ := d["uid"].(string)
		if uid == "" {
			uid = "uid-" + time.Now().Format("150405.000000000")
			d["uid"] = uid
		}
		m.data[col] = append(m.data[col], d)
		added = append(added, uid)
	}
	return db.WriteResult{Added: added}, nil
}

func (m *memDB) ReplaceOne(_ context.Context, col string, match db.Document, doc db.Document, _ bool) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	uid, _ := match["uid"].(string)
	for i, d := range m.data[col] {
		if d["uid"] == uid {
			doc["uid"] = uid
			m.data[col][i] = doc
			return 1, nil
		}
	}
	return 0, nil
}

func (m *memDB) UpdateOne(_ context.Context, col, uid string, patch db.Document, _ bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, d := range m.data[col] {
		if d["uid"] == uid {
			for k, v := range patch {
				d[k] = v
			}
			return nil
		}
	}
	return errors.New("not found")
}

func (m *memDB) Delete(_ context.Context, col string, cond condition.Cond, _ bool) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	kept := make([]db.Document, 0, len(m.data[col]))
	deleted := 0
	for _, d := range m.data[col] {
		if !cond.IsZero() && condition.Match(d, cond) {
			deleted++
			continue
		}
		kept = append(kept, d)
	}
	m.data[col] = kept
	return deleted, nil
}

// Stubs for the rest of the Driver surface — none are touched by the CRUD
// handlers under test.

func (m *memDB) BulkIncrement(context.Context, string, []db.IncrementOp, bool) error {
	return errUnsup
}
func (m *memDB) IncMany(context.Context, string, string, condition.Cond, int64) (int, error) {
	return 0, errUnsup
}
func (m *memDB) SetFields(context.Context, string, db.Document, condition.Cond) (int, error) {
	return 0, errUnsup
}
func (m *memDB) UnsetFields(context.Context, string, []string, condition.Cond) (int, error) {
	return 0, errUnsup
}
func (m *memDB) AppendList(context.Context, string, map[string][]any, condition.Cond) (int, error) {
	return 0, errUnsup
}
func (m *memDB) PrependList(context.Context, string, map[string][]any, condition.Cond) (int, error) {
	return 0, errUnsup
}
func (m *memDB) RemoveList(context.Context, string, map[string][]any, condition.Cond) (int, error) {
	return 0, errUnsup
}
func (m *memDB) CreateIndex(context.Context, string, []string) error { return nil }
func (m *memDB) ListCollections(context.Context) ([]string, error)   { return nil, nil }
func (m *memDB) Drop(context.Context, string) error                  { return nil }
func (m *memDB) Backup(context.Context, string, []string) error      { return nil }
func (m *memDB) CleanupTimeout(context.Context, string) (int, error) { return 0, nil }
func (m *memDB) CleanupComments(context.Context) (int, error)        { return 0, nil }
func (m *memDB) CleanupOrphans(context.Context, string) (int, error) { return 0, nil }
func (m *memDB) CleanupAuditLogs(context.Context, time.Duration) (int, error) {
	return 0, nil
}
func (m *memDB) CleanupSnooze(context.Context) (int, error)       { return 0, nil }
func (m *memDB) CleanupNotification(context.Context) (int, error) { return 0, nil }
func (m *memDB) ComputeStats(context.Context, string, time.Time, time.Time, string) ([]db.StatsBucket, error) {
	return nil, nil
}
func (m *memDB) Watcher() syncer.Bus { return nil }
func (m *memDB) Close() error        { return nil }

// nullHost is a Host implementation that returns minimal/no-op deps. Plugins
// in this package's tests never invoke Bus, Tracer, Metrics or Config, so
// those are wired to safe defaults rather than full stubs.
type nullHost struct {
	driver db.Driver
	logger *slog.Logger
	cfg    *config.Config
	metr   *telemetry.Registry
	tracer trace.Tracer
	plugs  map[string]Plugin
}

func newNullHost(driver db.Driver) *nullHost {
	return &nullHost{
		driver: driver,
		logger: slog.Default(),
		cfg:    config.Default(),
		metr:   telemetry.NewRegistry(nil),
		tracer: otel.Tracer("plugins-test"),
		plugs:  map[string]Plugin{},
	}
}

func (h *nullHost) DB() db.Driver                { return h.driver }
func (h *nullHost) Bus() Bus                     { return nil }
func (h *nullHost) Logger() *slog.Logger         { return h.logger }
func (h *nullHost) Tracer() trace.Tracer         { return h.tracer }
func (h *nullHost) Metrics() *telemetry.Registry { return h.metr }
func (h *nullHost) Config() *config.Config       { return h.cfg }
func (h *nullHost) Plugin(name string) Plugin    { return h.plugs[name] }
