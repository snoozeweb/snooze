package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/db/sqlite"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

func inputsHarness(t *testing.T) (chi.Router, db.Driver) {
	t.Helper()
	ctx := snoozetypes.WithTenant(context.Background(), snoozetypes.DefaultTenant)
	d, err := sqlite.New(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "snooze.db")})
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	rt := &Router{DB: d}
	r := chi.NewRouter()
	rt.mountInputs(r)
	return r, d
}

func TestInputs_AggregatesBySource(t *testing.T) {
	t.Parallel()
	r, d := inputsHarness(t)
	ctx := snoozetypes.WithTenant(context.Background(), snoozetypes.DefaultTenant)
	_, err := d.Write(ctx, "record", []db.Document{
		{"host": "h1", "source": "grafana", "date_epoch": int64(1000)},
		{"host": "h2", "source": "grafana", "date_epoch": int64(1050)},
		{"host": "h3", "source": "syslog", "date_epoch": int64(1200)},
	}, db.WriteOptions{UpdateTime: false})
	require.NoError(t, err)

	req := authReq("GET", "/api/v1/inputs?since=0", nil, "ro_stats")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var body struct {
		Data []db.SourceActivity `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	got := map[string]db.SourceActivity{}
	for _, row := range body.Data {
		got[row.Source] = row
	}
	require.Equal(t, int64(1050), got["grafana"].LastEpoch)
	require.Equal(t, int64(2), got["grafana"].Count)
	require.Equal(t, int64(1200), got["syslog"].LastEpoch)
}

func TestInputs_RequiresStatsPerm(t *testing.T) {
	t.Parallel()
	r, _ := inputsHarness(t)

	req := authReq("GET", "/api/v1/inputs", nil, "ro_record") // wrong perm
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code)

	req = httptest.NewRequest(http.MethodGet, "/api/v1/inputs", nil) // no claims
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)

	req = authReq("GET", "/api/v1/inputs?since=0", nil, "rw_stats")
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestInputs_BadSince(t *testing.T) {
	t.Parallel()
	r, _ := inputsHarness(t)
	req := authReq("GET", "/api/v1/inputs?since=notanumber", nil, "ro_stats")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestInputs_NegativeSince(t *testing.T) {
	t.Parallel()
	r, _ := inputsHarness(t)
	req := authReq("GET", "/api/v1/inputs?since=-1", nil, "ro_stats")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestInputs_DefaultWindowExcludesOldSources(t *testing.T) {
	t.Parallel()
	r, d := inputsHarness(t)
	ctx := snoozetypes.WithTenant(context.Background(), snoozetypes.DefaultTenant)
	now := time.Now().Unix()
	_, err := d.Write(ctx, "record", []db.Document{
		{"host": "old", "source": "ancient", "date_epoch": now - 35*24*3600}, // > 30d → excluded
		{"host": "new", "source": "recent", "date_epoch": now - 5*24*3600},   // < 30d → included
	}, db.WriteOptions{UpdateTime: false})
	require.NoError(t, err)

	req := authReq("GET", "/api/v1/inputs", nil, "ro_stats") // no ?since → 30-day default
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var body struct {
		Data []db.SourceActivity `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	got := map[string]db.SourceActivity{}
	for _, row := range body.Data {
		got[row.Source] = row
	}
	_, hasAncient := got["ancient"]
	require.False(t, hasAncient, "source older than the 30-day default window must be excluded")
	require.Equal(t, int64(1), got["recent"].Count)
}

// The read catch-all must open a bespoke ro_* route exactly as it opens a
// plugin CRUD read route — the SPA shows the Inputs page to an ro_all-only
// caller on that assumption. rw_stats-style writes stay out of reach.
func TestInputs_ReadAllCatchAllGrantsAccess(t *testing.T) {
	t.Parallel()
	r, _ := inputsHarness(t)

	req := authReq("GET", "/api/v1/inputs?since=0", nil, "ro_all")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}
