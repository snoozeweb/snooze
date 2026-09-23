package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/config"
	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// fakeRuntimeStore is a minimal config.RuntimeStore used by the config-route
// tests. GetSection JSON-round-trips the configured `console` section payload
// into dst, mirroring the production settings plugin. Every other method is a
// no-op returning empty.
type fakeRuntimeStore struct {
	// console is the payload returned for GetSection(ctx, "console", dst). A
	// nil map decodes to nothing (no override).
	console map[string]any
	// err, when non-nil, is returned by GetSection unconditionally instead of
	// decoding console — used to exercise handleConfig's error handling (the
	// ErrNoTenant anonymous-fallback vs. a genuine 500).
	err error
}

func (f *fakeRuntimeStore) Get(context.Context, string, string) (any, bool, error) {
	return nil, false, nil
}

func (f *fakeRuntimeStore) GetSection(_ context.Context, section string, dst any) error {
	if f.err != nil {
		return f.err
	}
	if section != "console" || f.console == nil {
		return nil
	}
	raw, err := json.Marshal(f.console)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, dst)
}

func (f *fakeRuntimeStore) Set(context.Context, string, string, any) error { return nil }
func (f *fakeRuntimeStore) Replace(context.Context, string, map[string]any) error {
	return nil
}

func (f *fakeRuntimeStore) Watch(_ context.Context, _ string) (<-chan config.RuntimeChange, error) {
	ch := make(chan config.RuntimeChange)
	close(ch)
	return ch, nil
}

// getConfig drives a GET /api/v1/config through a freshly-mounted router and
// decodes the {"data": …} envelope into a ConsoleConfig.
func getConfig(t *testing.T, store config.RuntimeStore) (int, ConsoleConfig) {
	t.Helper()
	rt := &Router{RuntimeStore: store}
	r := chi.NewRouter()
	rt.mountConfig(r)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/config", nil))
	var env struct {
		Data ConsoleConfig `json:"data"`
	}
	if rec.Code == http.StatusOK {
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	}
	return rec.Code, env.Data
}

func TestConfig_DefaultsWhenNoOverride(t *testing.T) {
	code, cfg := getConfig(t, &fakeRuntimeStore{})
	require.Equal(t, http.StatusOK, code)

	// The Plan 21 ladder is the source of truth: critical → rank 2.
	require.Equal(t, 2, cfg.SeverityRanks["critical"])
	// severity_order is rank-sorted (most severe first); rank 0 is an
	// emergency-class label.
	require.NotEmpty(t, cfg.SeverityOrder)
	require.Equal(t, 0, cfg.SeverityRanks[cfg.SeverityOrder[0]],
		"first ordered label must carry the most-severe (rank 0) emergency rank")
	// Scalar defaults mirror the current frontend hardcodes.
	require.Equal(t, 5, cfg.RefreshSecs)
	require.Equal(t, "-date_epoch", cfg.SortBy)
}

func TestConfig_OverrideMergesRanksAndScalars(t *testing.T) {
	store := &fakeRuntimeStore{console: map[string]any{
		"refresh_interval": 30,
		"columns":          []string{"severity", "host"},
		"severity_ranks":   map[string]any{"p1": 2},
	}}
	code, cfg := getConfig(t, store)
	require.Equal(t, http.StatusOK, code)

	// Present scalar replaced.
	require.Equal(t, 30, cfg.RefreshSecs)
	// Slice replaced wholesale.
	require.Equal(t, []string{"severity", "host"}, cfg.Columns)
	// SeverityRanks merges key-by-key: ladder defaults survive AND p1 is added.
	require.Equal(t, 2, cfg.SeverityRanks["critical"], "ladder default must survive the merge")
	require.Equal(t, 2, cfg.SeverityRanks["p1"], "operator addition must be merged in")
	// Untouched scalar keeps its default.
	require.Equal(t, "-date_epoch", cfg.SortBy)

	// severity_order is recomputed: p1 (rank 2) sits within the rank-2 block
	// alongside critical, ahead of every rank-3+ label and behind every
	// rank<2 label. Every label between p1 and critical also carries rank 2,
	// so p1 is "adjacent" to critical in the severity sense (same bucket).
	idxP1 := indexOf(cfg.SeverityOrder, "p1")
	idxCritical := indexOf(cfg.SeverityOrder, "critical")
	idxAlert := indexOf(cfg.SeverityOrder, "alert") // rank 1
	idxError := indexOf(cfg.SeverityOrder, "error") // rank 3
	require.GreaterOrEqual(t, idxP1, 0, "p1 must appear in severity_order")
	require.Less(t, idxAlert, idxP1, "p1 (rank 2) must sort after alert (rank 1)")
	require.Less(t, idxP1, idxError, "p1 (rank 2) must sort before error (rank 3)")
	lo, hi := idxP1, idxCritical
	if lo > hi {
		lo, hi = hi, lo
	}
	for i := lo; i <= hi; i++ {
		require.Equal(t, 2, cfg.SeverityRanks[cfg.SeverityOrder[i]],
			"every label between p1 and critical must be rank 2 (same severity block)")
	}
}

// TestConfig_ErrNoTenantFallsBackToDefaults covers the anonymous-caller path:
// GetSection fails closed with snoozetypes.ErrNoTenant (a naked/no-tenant
// context — exactly what an unauthenticated request through OptionalAuth
// produces) and handleConfig must serve pure code defaults with a 200,
// not a 500.
func TestConfig_ErrNoTenantFallsBackToDefaults(t *testing.T) {
	store := &fakeRuntimeStore{err: snoozetypes.ErrNoTenant}
	code, cfg := getConfig(t, store)
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, 2, cfg.SeverityRanks["critical"], "must fall back to pure code defaults")
	require.Equal(t, 5, cfg.RefreshSecs)
}

// TestConfig_OtherStoreErrorIs500 asserts that a GetSection failure NOT
// wrapping ErrNoTenant (a genuine storage/decode failure) still 500s — only
// the no-tenant case gets the anonymous-fallback treatment.
func TestConfig_OtherStoreErrorIs500(t *testing.T) {
	store := &fakeRuntimeStore{err: errors.New("boom: storage unavailable")}
	code, _ := getConfig(t, store)
	require.Equal(t, http.StatusInternalServerError, code)
}

// TestConfig_AuthenticatedCallerGetsOverlay: a request bearing a valid JWT
// still reaches the same overlay-applying code path as an anonymous request
// (OptionalAuth resolving the token must not disturb handleConfig's normal
// behaviour — this is the route wired with the token engine / key store from
// Router, mirroring how Build() wires the strict Auth middleware).
func TestConfig_AuthenticatedCallerGetsOverlay(t *testing.T) {
	eng := testTokenEngine(t)
	tok, _, err := eng.Sign(snoozetypes.Claims{Subject: "alice", Method: "local", TenantID: "acme"})
	require.NoError(t, err)

	store := &fakeRuntimeStore{console: map[string]any{"refresh_interval": 42}}
	rt := &Router{RuntimeStore: store, Auth: eng}
	r := chi.NewRouter()
	rt.mountConfig(r)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/config", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var env struct {
		Data ConsoleConfig `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	require.Equal(t, 42, env.Data.RefreshSecs, "overlay must still apply for an authenticated caller")
}

// TestConfig_InvalidTokenStillServesDefaults: a garbage Bearer token on
// /api/v1/config must not 401 (OptionalAuth's whole point) — it falls back to
// the same anonymous/no-tenant path as no header at all.
func TestConfig_InvalidTokenStillServesDefaults(t *testing.T) {
	rt := &Router{Auth: testTokenEngine(t)}
	r := chi.NewRouter()
	rt.mountConfig(r)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/config", nil)
	req.Header.Set("Authorization", "Bearer garbage")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestConfig_ReachableWithoutAuth(t *testing.T) {
	rt := &Router{}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/config", nil)
	require.True(t, rt.skipAuth(req),
		"GET /api/v1/config must bypass the auth middleware so the login screen can bootstrap branding")
}

func indexOf(ss []string, want string) int {
	for i, s := range ss {
		if s == want {
			return i
		}
	}
	return -1
}

// TestConfig_DefaultColumnsOwnerAfterState pins the ownership column's slot in
// the default layout: right after `state`, so "who is on it" reads next to
// "what state is it in".
func TestConfig_DefaultColumnsOwnerAfterState(t *testing.T) {
	code, cfg := getConfig(t, &fakeRuntimeStore{})
	require.Equal(t, http.StatusOK, code)
	idxState := indexOf(cfg.Columns, "state")
	require.GreaterOrEqual(t, idxState, 0)
	require.Equal(t, idxState+1, indexOf(cfg.Columns, "owner"), "owner must follow state")
}

// TestConfig_DefaultColumnsMatchSettingsMetadata enforces the "must match"
// note on defaultColumns: the settings catalogue advertises the same default
// to the Settings → Console editor. The YAML is read from disk rather than by
// importing the settings plugin, which this package must never import.
func TestConfig_DefaultColumnsMatchSettingsMetadata(t *testing.T) {
	raw, err := os.ReadFile("../pluginimpl/settings/metadata.yaml")
	require.NoError(t, err)
	meta, err := plugins.ParseMetadata(raw)
	require.NoError(t, err)
	field, ok := meta.SettingForm.Get("columns")
	require.True(t, ok, "settings metadata must declare the columns setting")
	def, _ := field.(map[string]any)["default_value"].([]any)
	got := make([]string, 0, len(def))
	for _, v := range def {
		got = append(got, v.(string))
	}
	require.Equal(t, defaultColumns, got)
}
