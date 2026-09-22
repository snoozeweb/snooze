package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/housekeeper"
)

// hkRouter builds a chi router with only the housekeeping subtree mounted,
// wiring the supplied housekeeper (nil is allowed to exercise the 503 path).
func hkRouter(t *testing.T, hk *housekeeper.Housekeeper) chi.Router {
	t.Helper()
	rt := &Router{HK: hk}
	r := chi.NewRouter()
	rt.mountHousekeeping(r)
	return r
}

// hkWith registers a single named job (returning err) and returns the
// housekeeper. err may be nil for a successful job.
func hkWith(t *testing.T, name string, err error) *housekeeper.Housekeeper {
	t.Helper()
	hk := housekeeper.New(nil)
	require.NoError(t, hk.Register(housekeeper.NewJobFunc(name, func(context.Context) error {
		return err
	}), housekeeper.Schedule{Interval: time.Hour}))
	return hk
}

func TestHousekeepingRun_NoHK_503(t *testing.T) {
	r := hkRouter(t, nil)
	req := authReq(http.MethodPost, "/api/v1/housekeeping/run", nil, auth.AllPermission)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestHousekeepingRun_WithHK_200(t *testing.T) {
	r := hkRouter(t, hkWith(t, "cleanup_ok", nil))
	req := authReq(http.MethodPost, "/api/v1/housekeeping/run", nil, auth.AllPermission)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var body struct {
		Status string                  `json:"status"`
		Jobs   []housekeeper.JobResult `json:"jobs"`
		Errors int                     `json:"errors"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "ok", body.Status)
	require.Len(t, body.Jobs, 1)
	require.Equal(t, "cleanup_ok", body.Jobs[0].Name)
	require.Empty(t, body.Jobs[0].Error)
	require.Equal(t, 0, body.Errors)
}

func TestHousekeepingRun_JobError_200WithErrorCount(t *testing.T) {
	r := hkRouter(t, hkWith(t, "cleanup_bad", errors.New("boom")))
	req := authReq(http.MethodPost, "/api/v1/housekeeping/run", nil, auth.AllPermission)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var body struct {
		Status string                  `json:"status"`
		Jobs   []housekeeper.JobResult `json:"jobs"`
		Errors int                     `json:"errors"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "ok", body.Status)
	require.Len(t, body.Jobs, 1)
	require.Equal(t, "boom", body.Jobs[0].Error)
	require.Equal(t, 1, body.Errors)
}

func TestHousekeepingStatus_NoHK_503(t *testing.T) {
	r := hkRouter(t, nil)
	req := authReq(http.MethodGet, "/api/v1/housekeeping/status", nil, auth.AllPermission)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestHousekeepingStatus_WithHK_200(t *testing.T) {
	hk := housekeeper.New(nil)
	require.NoError(t, hk.Register(housekeeper.NewJobFunc("a", func(context.Context) error { return nil }), housekeeper.Schedule{Interval: time.Hour}))
	require.NoError(t, hk.Register(housekeeper.NewJobFunc("b", func(context.Context) error { return nil }), housekeeper.Schedule{Interval: time.Hour}))

	r := hkRouter(t, hk)
	req := authReq(http.MethodGet, "/api/v1/housekeeping/status", nil, auth.AllPermission)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var body struct {
		Status         string `json:"status"`
		RegisteredJobs int    `json:"registered_jobs"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "ok", body.Status)
	require.Equal(t, 2, body.RegisteredJobs)
}

func TestHousekeepingRun_NoAuth_401(t *testing.T) {
	r := hkRouter(t, hkWith(t, "cleanup_ok", nil))
	// No claims on the context → RequirePerm yields 401.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/housekeeping/run", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestHousekeepingRun_WrongPerm_403(t *testing.T) {
	r := hkRouter(t, hkWith(t, "cleanup_ok", nil))
	// A read-only permission must not satisfy the rw_all gate — neither a
	// collection one nor the read catch-all, which covers ro_* wants only.
	for _, perm := range []string{"ro_record", "ro_all"} {
		req := authReq(http.MethodPost, "/api/v1/housekeeping/run", nil, perm)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equalf(t, http.StatusForbidden, rec.Code, "caller holding %s", perm)
	}
}
