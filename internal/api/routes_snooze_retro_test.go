package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/japannext/snooze/internal/auth"
	"github.com/japannext/snooze/internal/condition"
	"github.com/japannext/snooze/internal/db"
	"github.com/japannext/snooze/internal/db/sqlite"
	"github.com/japannext/snooze/pkg/snoozetypes"
)

// noCond returns the always-true condition for full-collection reads.
func noCond() condition.Cond { return condition.Cond{} }

// authReq wraps an httptest request with claims granting the supplied
// permissions, so it passes the RequirePerm gate.
func authReq(method, target string, body []byte, perms ...string) *http.Request {
	req := httptest.NewRequest(method, target, bytes.NewReader(body))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	ctx := auth.WithClaims(req.Context(), snoozetypes.Claims{
		Subject:     "tester",
		Method:      "local",
		Permissions: perms,
	})
	return req.WithContext(ctx)
}

// retroApplyHarness brings up a SQLite-backed Router with just enough wiring
// to exercise the retro-apply route. We don't load the full plugin set —
// the route only needs DB access.
func retroApplyHarness(t *testing.T) (chi.Router, db.Driver) {
	t.Helper()
	ctx := context.Background()
	d, err := sqlite.New(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "snooze.db")})
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })

	rt := &Router{DB: d}
	r := chi.NewRouter()
	rt.mountSnoozeRetro(r)
	return r, d
}

func writeJSONReq(t *testing.T, r chi.Router, method, target string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf []byte
	if body != nil {
		var err error
		buf, err = json.Marshal(body)
		require.NoError(t, err)
	}
	req := authReq(method, target, buf, "rw_record")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestRetroApply_TagsMatchingRecords(t *testing.T) {
	t.Parallel()
	r, d := retroApplyHarness(t)
	ctx := context.Background()

	// Seed a non-discard snooze + a few records (one matches).
	res, err := d.Write(ctx, "snooze", []db.Document{{
		"name":      "weekend-noise",
		"condition": []any{"=", "host", "noisy-1"},
		"discard":   false,
	}}, db.WriteOptions{})
	require.NoError(t, err)
	uid := res.Added[0]

	_, err = d.Write(ctx, "record", []db.Document{
		{"host": "noisy-1", "message": "one"},
		{"host": "noisy-1", "message": "two"},
		{"host": "quiet-1", "message": "skip"},
	}, db.WriteOptions{})
	require.NoError(t, err)

	rec := writeJSONReq(t, r, "POST", "/api/v1/snooze/"+uid+"/retro_apply", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	var body retroApplyResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, 2, body.Matched)
	require.Equal(t, 2, body.Tagged)
	require.Equal(t, 0, body.Deleted)
	require.Equal(t, "weekend-noise", body.Snooze)

	// The two matching records now carry snoozed=name; the third doesn't.
	all, _, err := d.Search(ctx, "record", noCond(), db.Page{})
	require.NoError(t, err)
	var tagged, untagged int
	for _, doc := range all {
		if doc["snoozed"] == "weekend-noise" {
			tagged++
		} else {
			untagged++
		}
	}
	require.Equal(t, 2, tagged)
	require.Equal(t, 1, untagged)
}

func TestRetroApply_DeletesWhenDiscard(t *testing.T) {
	t.Parallel()
	r, d := retroApplyHarness(t)
	ctx := context.Background()

	res, err := d.Write(ctx, "snooze", []db.Document{{
		"name":      "drop-flap",
		"condition": []any{"=", "source", "flap"},
		"discard":   true,
	}}, db.WriteOptions{})
	require.NoError(t, err)
	uid := res.Added[0]

	_, err = d.Write(ctx, "record", []db.Document{
		{"source": "flap", "host": "a"},
		{"source": "flap", "host": "b"},
		{"source": "real", "host": "c"},
	}, db.WriteOptions{})
	require.NoError(t, err)

	rec := writeJSONReq(t, r, "POST", "/api/v1/snooze/"+uid+"/retro_apply", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	var body retroApplyResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, 2, body.Matched)
	require.Equal(t, 2, body.Deleted)
	require.Equal(t, 0, body.Tagged)

	all, _, err := d.Search(ctx, "record", noCond(), db.Page{})
	require.NoError(t, err)
	require.Len(t, all, 1)
	require.Equal(t, "real", all[0]["source"])
}

func TestRetroApply_UnknownUidReturns404(t *testing.T) {
	t.Parallel()
	r, _ := retroApplyHarness(t)

	rec := writeJSONReq(t, r, "POST", "/api/v1/snooze/does-not-exist/retro_apply", nil)
	require.Equal(t, http.StatusNotFound, rec.Code)
}

// TestRetroApply_RequiresRwRecord pins that callers without rw_record (or
// the rw_all wildcard) get a 403 — the route writes/deletes records, so
// read-only auditors must not be able to fire it.
func TestRetroApply_RequiresRwRecord(t *testing.T) {
	t.Parallel()
	r, d := retroApplyHarness(t)
	ctx := context.Background()

	res, err := d.Write(ctx, "snooze", []db.Document{{
		"name":      "any",
		"condition": []any{"=", "host", "z"},
	}}, db.WriteOptions{})
	require.NoError(t, err)
	uid := res.Added[0]

	// Caller with only ro_record → 403.
	req := authReq("POST", "/api/v1/snooze/"+uid+"/retro_apply", nil, "ro_record")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code)

	// Caller with rw_all (the admin wildcard) → 200.
	req = authReq("POST", "/api/v1/snooze/"+uid+"/retro_apply", nil, "rw_all")
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	// No claims at all → 401.
	req = httptest.NewRequest(http.MethodPost, "/api/v1/snooze/"+uid+"/retro_apply", nil)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestRetroApply_BumpsHitCount(t *testing.T) {
	t.Parallel()
	r, d := retroApplyHarness(t)
	ctx := context.Background()

	res, err := d.Write(ctx, "snooze", []db.Document{{
		"name":      "bump",
		"condition": []any{"=", "host", "x"},
		"discard":   false,
		"hits":      int64(5),
	}}, db.WriteOptions{})
	require.NoError(t, err)
	uid := res.Added[0]

	_, err = d.Write(ctx, "record", []db.Document{
		{"host": "x"},
		{"host": "x"},
		{"host": "x"},
	}, db.WriteOptions{})
	require.NoError(t, err)

	rec := writeJSONReq(t, r, "POST", "/api/v1/snooze/"+uid+"/retro_apply", nil)
	require.Equal(t, http.StatusOK, rec.Code)

	got, err := d.GetOne(ctx, "snooze", db.Document{"uid": uid})
	require.NoError(t, err)
	// Allow either int64 or float64 since JSON / SQLite can hand it back as either.
	var hits int64
	if v, ok := got["hits"].(int64); ok {
		hits = v
	} else if f, ok := got["hits"].(float64); ok {
		hits = int64(f)
	}
	require.Equal(t, int64(5+3), hits)
}
