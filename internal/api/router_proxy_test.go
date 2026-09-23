package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/config"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/internal/syncer"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// memDriver is a small in-memory db.Driver for the auth-proxy wiring +
// self-escalation tests. It implements Search/GetOne/Write with primary-key
// upsert (mirroring the auth package's fake_db_test.go); every other method is
// a benign no-op.
type memDriver struct {
	mu          sync.Mutex
	collections map[string][]db.Document
}

func newMemDriver() *memDriver { return &memDriver{collections: map[string][]db.Document{}} }

func (m *memDriver) seed(coll string, docs ...db.Document) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, d := range docs {
		cp := cloneMemDoc(d)
		if _, ok := cp["uid"]; !ok {
			cp["uid"] = uuid.NewString()
		}
		m.collections[coll] = append(m.collections[coll], cp)
	}
}

func cloneMemDoc(d db.Document) db.Document {
	cp := make(db.Document, len(d))
	for k, v := range d {
		cp[k] = v
	}
	return cp
}

func docMatches(doc, filter db.Document) bool {
	for k, want := range filter {
		got, ok := doc[k]
		if !ok || got != want {
			return false
		}
	}
	return true
}

func (m *memDriver) Search(_ context.Context, coll string, _ condition.Cond, _ db.Page) ([]db.Document, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rows := m.collections[coll]
	out := make([]db.Document, len(rows))
	copy(out, rows)
	return out, len(out), nil
}

func (m *memDriver) GetOne(_ context.Context, coll string, match db.Document) (db.Document, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, doc := range m.collections[coll] {
		if docMatches(doc, match) {
			return cloneMemDoc(doc), nil
		}
	}
	return nil, db.ErrNotFound
}

func (m *memDriver) Write(_ context.Context, coll string, docs []db.Document, opts db.WriteOptions) (db.WriteResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var res db.WriteResult
	for _, doc := range docs {
		var match db.Document
		if len(opts.Primary) > 0 {
			match = make(db.Document, len(opts.Primary))
			for _, k := range opts.Primary {
				match[k] = doc[k]
			}
		} else if uid, ok := doc["uid"].(string); ok && uid != "" {
			match = db.Document{"uid": uid}
		}
		idx := -1
		for i, existing := range m.collections[coll] {
			if docMatches(existing, match) {
				idx = i
				break
			}
		}
		if idx >= 0 {
			for k, v := range doc {
				m.collections[coll][idx][k] = v
			}
			res.Updated = append(res.Updated, m.collections[coll][idx]["uid"].(string))
		} else {
			cp := cloneMemDoc(doc)
			if _, ok := cp["uid"]; !ok {
				cp["uid"] = uuid.NewString()
			}
			m.collections[coll] = append(m.collections[coll], cp)
			res.Added = append(res.Added, cp["uid"].(string))
		}
	}
	return res, nil
}

func (m *memDriver) Convert(context.Context, condition.Cond, []string) (db.DriverQuery, error) {
	return nil, nil
}
func (m *memDriver) ReplaceOne(context.Context, string, db.Document, db.Document, bool) (int, error) {
	return 0, nil
}
func (m *memDriver) UpdateOne(context.Context, string, string, db.Document, bool) error { return nil }
func (m *memDriver) Delete(context.Context, string, condition.Cond, bool) (int, error) {
	return 0, nil
}
func (m *memDriver) BulkIncrement(context.Context, string, []db.IncrementOp, bool) error { return nil }
func (m *memDriver) IncMany(context.Context, string, string, condition.Cond, int64) (int, error) {
	return 0, nil
}
func (m *memDriver) SetFields(context.Context, string, db.Document, condition.Cond) (int, error) {
	return 0, nil
}
func (m *memDriver) UnsetFields(context.Context, string, []string, condition.Cond) (int, error) {
	return 0, nil
}
func (m *memDriver) AppendList(context.Context, string, map[string][]any, condition.Cond) (int, error) {
	return 0, nil
}
func (m *memDriver) PrependList(context.Context, string, map[string][]any, condition.Cond) (int, error) {
	return 0, nil
}
func (m *memDriver) RemoveList(context.Context, string, map[string][]any, condition.Cond) (int, error) {
	return 0, nil
}
func (m *memDriver) CreateIndex(context.Context, string, []string) error { return nil }
func (m *memDriver) ListCollections(context.Context) ([]string, error)   { return nil, nil }
func (m *memDriver) Drop(context.Context, string) error                  { return nil }
func (m *memDriver) Backup(context.Context, string, []string) error      { return nil }
func (m *memDriver) CleanupTimeout(context.Context, string) (int, error) { return 0, nil }
func (m *memDriver) CleanupComments(context.Context) (int, error)        { return 0, nil }
func (m *memDriver) CleanupOrphans(context.Context, string) (int, error) { return 0, nil }
func (m *memDriver) CleanupAuditLogs(context.Context, time.Duration) (int, error) {
	return 0, nil
}
func (m *memDriver) CleanupSnooze(context.Context) (int, error)       { return 0, nil }
func (m *memDriver) CleanupNotification(context.Context) (int, error) { return 0, nil }
func (m *memDriver) ComputeStats(context.Context, string, time.Time, time.Time, string) ([]db.StatsBucket, error) {
	return nil, nil
}
func (m *memDriver) CountBy(context.Context, string, condition.Cond, string) (map[string]int, error) {
	return map[string]int{}, nil
}
func (m *memDriver) Watcher() syncer.Bus { return nil }
func (m *memDriver) Close() error        { return nil }

// proxyEnabledConfig returns a Default() config with the auth-proxy mode on.
func proxyEnabledConfig() *config.Config {
	cfg := config.Default()
	cfg.AuthProxy.Enabled = true
	return cfg
}

// TestBuild_AuthProxyPeerIPGate locks the mount order: the trust gate must
// compare against the genuine TCP peer (captured by CapturePeerIP BEFORE chi
// RealIP rewrites RemoteAddr), so a spoofed X-Forwarded-For from an untrusted
// peer is rejected even through the FULL production chain (RealIP present).
//
// httptest.NewServer dials from loopback, so the genuine peer is 127.0.0.1.
//   - Spoof sub-case: trusted=10.0.0.0/8, XFF=10.0.0.5 → the real peer (127.x)
//     is untrusted; the request must fall through to 401 and NOT provision a
//     JIT user. (Note: chi RealIP rewrites RemoteAddr to 10.0.0.5 here — a
//     RemoteAddr-only gate would have been fooled; capturing before RealIP is
//     exactly what prevents that amplification.)
//   - Genuine sub-case: trusted=127.0.0.0/8 (the real loopback peer) → the
//     proxy path authenticates through the full chain and provisions the user.
func TestBuild_AuthProxyPeerIPGate(t *testing.T) {
	seedRole := func(drv *memDriver) {
		drv.seed(auth.RoleCollection, db.Document{
			"name":        "operator",
			"tenant_id":   snoozetypes.DefaultTenant,
			"permissions": []string{"ro_rule"},
			"groups":      []string{"ops"},
		})
	}

	t.Run("spoofed XFF from untrusted peer is rejected", func(t *testing.T) {
		drv := newMemDriver()
		seedRole(drv)
		cfg := proxyEnabledConfig()
		cfg.AuthProxy.TrustedProxies = []string{"10.0.0.0/8"} // loopback NOT in here
		rt := &Router{
			Auth:      testTokenEngine(t),
			DB:        drv,
			Config:    cfg,
			ProxyAuth: auth.NewProxyAuthenticator(drv, auth.NewRoleResolver(drv), "proxy"),
			Plugins:   map[string]plugins.Plugin{"record": &stubPlugin{name: "record"}},
		}
		srv := httptest.NewServer(rt.Build())
		defer srv.Close()

		req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/permissions", nil)
		require.NoError(t, err)
		req.Header.Set("X-Forwarded-For", "10.0.0.5") // spoofed trusted-proxy IP
		req.Header.Set("X-Forwarded-User", "root")
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		require.Equal(t, http.StatusUnauthorized, resp.StatusCode,
			"spoofed XFF from an untrusted TCP peer must 401, got %d", resp.StatusCode)

		// And no JIT user was created (the provisioner was never reached).
		_, err = drv.GetOne(context.Background(), auth.LocalCollection, db.Document{"name": "root", "method": "proxy"})
		require.ErrorIs(t, err, db.ErrNotFound,
			"spoofed XFF must not JIT-provision a user")
	})

	t.Run("genuine trusted peer authenticates through full chain", func(t *testing.T) {
		drv := newMemDriver()
		seedRole(drv)
		cfg := proxyEnabledConfig()
		cfg.AuthProxy.TrustedProxies = []string{"127.0.0.0/8"} // the real loopback peer
		rt := &Router{
			Auth:      testTokenEngine(t),
			DB:        drv,
			Config:    cfg,
			ProxyAuth: auth.NewProxyAuthenticator(drv, auth.NewRoleResolver(drv), "proxy"),
			Plugins:   map[string]plugins.Plugin{"record": &stubPlugin{name: "record"}},
		}
		srv := httptest.NewServer(rt.Build())
		defer srv.Close()

		req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/permissions", nil)
		require.NoError(t, err)
		req.Header.Set("X-Forwarded-User", "alice")
		req.Header.Set("X-Forwarded-Groups", "ops")
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		require.NotEqual(t, http.StatusUnauthorized, resp.StatusCode,
			"genuine trusted peer must authenticate through the full chain, got %d", resp.StatusCode)

		doc, err := drv.GetOne(context.Background(), auth.LocalCollection, db.Document{"name": "alice", "method": "proxy"})
		require.NoError(t, err)
		require.NotContains(t, doc, "password")
	})
}

// TestBuild_AuthProxyEnabledWiresMiddleware proves the conditional wiring: when
// the config enables the proxy mode and rt.ProxyAuth is set, a request carrying
// the proxy headers (no Authorization) reaches a mounted handler.
func TestBuild_AuthProxyEnabledWiresMiddleware(t *testing.T) {
	drv := newMemDriver()
	// Seed an "ops" -> "operator" mapping with a benign record permission.
	drv.seed(auth.RoleCollection, db.Document{
		"name":        "operator",
		"tenant_id":   snoozetypes.DefaultTenant,
		"permissions": []string{"ro_rule"},
		"groups":      []string{"ops"},
	})

	rt := &Router{
		Auth:      testTokenEngine(t),
		DB:        drv,
		Config:    proxyEnabledConfig(),
		ProxyAuth: auth.NewProxyAuthenticator(drv, auth.NewRoleResolver(drv), "proxy"),
		Plugins:   map[string]plugins.Plugin{"record": &stubPlugin{name: "record"}},
		Processor: &fakeProcessor{},
	}
	h := rt.Build()
	srv := httptest.NewServer(h)
	defer srv.Close()

	// /api/v1/permissions is an authenticated route in the chain; a proxy
	// header request (no Authorization) must authenticate and NOT 401.
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/permissions", nil)
	require.NoError(t, err)
	req.Header.Set("X-Forwarded-User", "alice")
	req.Header.Set("X-Forwarded-Groups", "ops")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.NotEqual(t, http.StatusUnauthorized, resp.StatusCode,
		"proxy header request should authenticate, got %d", resp.StatusCode)

	// The JIT user now exists with method=proxy and no password.
	doc, err := drv.GetOne(context.Background(), auth.LocalCollection, db.Document{"name": "alice", "method": "proxy"})
	require.NoError(t, err)
	require.NotContains(t, doc, "password")
}

// TestBuild_AuthProxyDisabledIgnoresHeaders proves byte-identical disabled
// behaviour at the router level: with the proxy disabled, a header-only request
// gets the normal 401.
func TestBuild_AuthProxyDisabledIgnoresHeaders(t *testing.T) {
	rt := &Router{
		Auth:    testTokenEngine(t),
		DB:      newMemDriver(),
		Config:  config.Default(), // AuthProxy disabled
		Plugins: map[string]plugins.Plugin{"record": &stubPlugin{name: "record"}},
	}
	h := rt.Build()
	srv := httptest.NewServer(h)
	defer srv.Close()

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/permissions", nil)
	require.NoError(t, err)
	req.Header.Set("X-Forwarded-User", "alice")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

// TestBuild_AuthProxyUserCannotSelfEscalate is the security invariant: a proxy
// user whose groups map only to a benign (non-platform) role must NOT be able
// to reach the platform-gated tenant registry. RoleResolver never grants
// ro_tenant/rw_tenant from a non-platform_admin group, so RequirePlatformPerm
// returns 403.
func TestBuild_AuthProxyUserCannotSelfEscalate(t *testing.T) {
	drv := newMemDriver()
	drv.seed(auth.RoleCollection, db.Document{
		"name":        "operator",
		"tenant_id":   snoozetypes.DefaultTenant,
		"permissions": []string{"rw_rule", "rw_all"}, // even rw_all must not satisfy a platform perm
		"groups":      []string{"ops"},
	})

	rt := &Router{
		Auth:      testTokenEngine(t),
		DB:        drv,
		Config:    proxyEnabledConfig(),
		ProxyAuth: auth.NewProxyAuthenticator(drv, auth.NewRoleResolver(drv), "proxy"),
		Plugins:   map[string]plugins.Plugin{"record": &stubPlugin{name: "record"}},
	}
	h := rt.Build()
	srv := httptest.NewServer(h)
	defer srv.Close()

	// A write to the tenant registry is platform-gated (rw_tenant, literal).
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/tenant", nil)
	require.NoError(t, err)
	req.Header.Set("X-Forwarded-User", "alice")
	req.Header.Set("X-Forwarded-Groups", "ops")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusForbidden, resp.StatusCode,
		"proxy user with no platform role must be 403 on /api/v1/tenant, got %d", resp.StatusCode)
}
