package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/syncer"
)

// fakeDB is a minimal db.Driver stub used by the health and metrics tests.
// Only ListCollections is exercised; every other method panics so a stray
// call is caught immediately.
type fakeDB struct {
	collections []string
	listErr     error
	nodes       []db.Document // returned by Search("nodes", …) for cluster status
}

func (f *fakeDB) Search(_ context.Context, coll string, _ condition.Cond, _ db.Page) ([]db.Document, int, error) {
	if coll == "nodes" {
		return f.nodes, len(f.nodes), nil
	}
	return nil, 0, nil
}
func (f *fakeDB) GetOne(_ context.Context, _ string, _ db.Document) (db.Document, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeDB) Convert(_ context.Context, _ condition.Cond, _ []string) (db.DriverQuery, error) {
	return nil, nil
}
func (f *fakeDB) Write(_ context.Context, _ string, _ []db.Document, _ db.WriteOptions) (db.WriteResult, error) {
	return db.WriteResult{}, nil
}
func (f *fakeDB) ReplaceOne(_ context.Context, _ string, _ db.Document, _ db.Document, _ bool) (int, error) {
	return 0, nil
}
func (f *fakeDB) UpdateOne(_ context.Context, _, _ string, _ db.Document, _ bool) error {
	return nil
}
func (f *fakeDB) Delete(_ context.Context, _ string, _ condition.Cond, _ bool) (int, error) {
	return 0, nil
}
func (f *fakeDB) BulkIncrement(_ context.Context, _ string, _ []db.IncrementOp, _ bool) error {
	return nil
}
func (f *fakeDB) IncMany(_ context.Context, _, _ string, _ condition.Cond, _ int64) (int, error) {
	return 0, nil
}
func (f *fakeDB) SetFields(_ context.Context, _ string, _ db.Document, _ condition.Cond) (int, error) {
	return 0, nil
}
func (f *fakeDB) AppendList(_ context.Context, _ string, _ map[string][]any, _ condition.Cond) (int, error) {
	return 0, nil
}
func (f *fakeDB) PrependList(_ context.Context, _ string, _ map[string][]any, _ condition.Cond) (int, error) {
	return 0, nil
}
func (f *fakeDB) RemoveList(_ context.Context, _ string, _ map[string][]any, _ condition.Cond) (int, error) {
	return 0, nil
}
func (f *fakeDB) CreateIndex(_ context.Context, _ string, _ []string) error { return nil }
func (f *fakeDB) ListCollections(_ context.Context) ([]string, error) {
	return f.collections, f.listErr
}
func (f *fakeDB) Drop(_ context.Context, _ string) error                  { return nil }
func (f *fakeDB) Backup(_ context.Context, _ string, _ []string) error    { return nil }
func (f *fakeDB) CleanupTimeout(_ context.Context, _ string) (int, error) { return 0, nil }
func (f *fakeDB) CleanupComments(_ context.Context) (int, error)          { return 0, nil }
func (f *fakeDB) CleanupOrphans(_ context.Context, _ string) (int, error) { return 0, nil }
func (f *fakeDB) CleanupAuditLogs(_ context.Context, _ time.Duration) (int, error) {
	return 0, nil
}
func (f *fakeDB) CleanupSnooze(_ context.Context) (int, error)       { return 0, nil }
func (f *fakeDB) CleanupNotification(_ context.Context) (int, error) { return 0, nil }
func (f *fakeDB) ComputeStats(_ context.Context, _ string, _, _ time.Time, _ string) ([]db.StatsBucket, error) {
	return nil, nil
}
func (f *fakeDB) RenumberField(_ context.Context, _, _ string) error { return nil }
func (f *fakeDB) Watcher() syncer.Bus                                { return nil }
func (f *fakeDB) Close() error                                       { return nil }

func TestHealthz_OK(t *testing.T) {
	rt := &Router{}
	r := chi.NewRouter()
	rt.mountHealth(r)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestReadyz_OK(t *testing.T) {
	rt := &Router{DB: &fakeDB{collections: []string{"record"}}}
	r := chi.NewRouter()
	rt.mountHealth(r)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestReadyz_DBError(t *testing.T) {
	rt := &Router{DB: &fakeDB{listErr: errors.New("nope")}}
	r := chi.NewRouter()
	rt.mountHealth(r)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestHealthVerbose(t *testing.T) {
	rt := &Router{DB: &fakeDB{collections: []string{"record"}}}
	r := chi.NewRouter()
	rt.mountHealth(r)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"db":"ok"`)
}

func TestClusterStatus_StandaloneWhenNodesEmpty(t *testing.T) {
	rt := &Router{DB: &fakeDB{}}
	r := chi.NewRouter()
	rt.mountHealth(r)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/cluster/status", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"name":"standalone"`)
	require.Contains(t, rec.Body.String(), `"leader":"standalone"`)
}

func TestClusterStatus_RealMembersGradedByFreshness(t *testing.T) {
	now := time.Now().UTC()
	rt := &Router{DB: &fakeDB{
		nodes: []db.Document{
			{"node": "node-a", "last_seen": now.Add(-2 * time.Second).Format(time.RFC3339Nano)},
			{"node": "node-b", "last_seen": now.Add(-30 * time.Second).Format(time.RFC3339Nano)},
			{"node": "node-c", "last_seen": now.Add(-5 * time.Minute).Format(time.RFC3339Nano)},
		},
	}}
	r := chi.NewRouter()
	rt.mountHealth(r)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/cluster/status", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	require.Contains(t, body, `"node-a"`)
	require.Contains(t, body, `"node-b"`)
	require.Contains(t, body, `"node-c"`)
	// node-a (fresh) → ok, node-b (30s) → degraded, node-c (5m) → down.
	// Leader is the first ok member alphabetically: node-a.
	require.Contains(t, body, `"leader":"node-a"`)
	require.Contains(t, body, `"status":"degraded"`)
	require.Contains(t, body, `"status":"down"`)
}
