package heartbeat

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/db"
)

// withURLParam returns a request carrying a chi route context that resolves
// chi.URLParam(r, "uid") to the supplied value, so a handler can be exercised
// directly without mounting a full router.
func withURLParam(req *http.Request, key, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

// statusListResp mirrors the list envelope with the projected status field on
// each document, so tests can read data[].status and meta.count.
type statusListResp struct {
	Data []map[string]any `json:"data"`
	Meta struct {
		Count  int `json:"count"`
		Limit  int `json:"limit"`
		Offset int `json:"offset"`
		Total  int `json:"total"`
	} `json:"meta"`
}

// seedFreshAndOverdue inserts two heartbeats relative to now: "fresh" pinged
// 10s ago (ok) and "stale" pinged 5m ago (overdue), both with a 60s interval.
func seedFreshAndOverdue(host *fakeHost, now time.Time) {
	host.driver.add(db.Document{
		"uid":       "fresh-uid",
		"name":      "fresh",
		"interval":  float64(60),
		"enabled":   true,
		"last_seen": now.Add(-10 * time.Second).UTC().Format(time.RFC3339),
	})
	host.driver.add(db.Document{
		"uid":       "stale-uid",
		"name":      "stale",
		"interval":  float64(60),
		"enabled":   true,
		"last_seen": now.Add(-5 * time.Minute).UTC().Format(time.RFC3339),
	})
}

// listRequest drives p.handleListHeartbeats(host) for the given raw query
// string (without a leading "?") and returns the decoded list envelope.
func listRequest(t *testing.T, p *Plugin, host *fakeHost, rawQuery string) (*httptest.ResponseRecorder, statusListResp) {
	t.Helper()
	target := "/api/v1/heartbeat"
	if rawQuery != "" {
		target += "?" + rawQuery
	}
	req := httptest.NewRequest(http.MethodGet, target, nil)
	w := httptest.NewRecorder()
	p.handleListHeartbeats(host)(w, req)

	var resp statusListResp
	if w.Body.Len() > 0 {
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	}
	return w, resp
}

func TestListHeartbeatsInjectsStatus(t *testing.T) {
	host := newHost()
	now := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)
	seedFreshAndOverdue(host, now)

	p := newPlugin(t, host)
	p.now = func() time.Time { return now }

	w, resp := listRequest(t, p, host, "")
	require.Equal(t, http.StatusOK, w.Code)
	require.Len(t, resp.Data, 2)

	byName := map[string]string{}
	for _, d := range resp.Data {
		name, _ := d["name"].(string)
		status, _ := d["status"].(string)
		byName[name] = status
	}
	require.Equal(t, StatusOK, byName["fresh"], "fresh heartbeat must be ok")
	require.Equal(t, StatusOverdue, byName["stale"], "stale heartbeat must be overdue")
}

func TestListHeartbeatsStatusFilter(t *testing.T) {
	host := newHost()
	now := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)
	seedFreshAndOverdue(host, now)

	p := newPlugin(t, host)
	p.now = func() time.Time { return now }

	w, resp := listRequest(t, p, host, "status=overdue")
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, 1, resp.Meta.Count, "only the overdue heartbeat must remain")
	require.Len(t, resp.Data, 1)
	require.Equal(t, "stale", resp.Data[0]["name"])
	require.Equal(t, StatusOverdue, resp.Data[0]["status"])
}

func TestGetOneHeartbeatInjectsStatus(t *testing.T) {
	host := newHost()
	now := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)
	host.driver.add(db.Document{
		"uid":       "stale-uid",
		"name":      "stale",
		"interval":  float64(60),
		"enabled":   true,
		"last_seen": now.Add(-5 * time.Minute).UTC().Format(time.RFC3339),
	})

	p := newPlugin(t, host)
	p.now = func() time.Time { return now }

	req := httptest.NewRequest(http.MethodGet, "/api/v1/heartbeat/stale-uid", nil)
	req = withURLParam(req, "uid", "stale-uid")
	w := httptest.NewRecorder()
	p.handleGetOneHeartbeat(host)(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var doc map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &doc))
	require.Equal(t, StatusOverdue, doc["status"])
	require.Equal(t, "stale", doc["name"])
}

func TestGetOneHeartbeatNotFound(t *testing.T) {
	host := newHost()
	now := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)

	p := newPlugin(t, host)
	p.now = func() time.Time { return now }

	req := httptest.NewRequest(http.MethodGet, "/api/v1/heartbeat/missing", nil)
	req = withURLParam(req, "uid", "missing")
	w := httptest.NewRecorder()
	p.handleGetOneHeartbeat(host)(w, req)

	require.Equal(t, http.StatusNotFound, w.Code)
}
