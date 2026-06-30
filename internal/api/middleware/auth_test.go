package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/config/schema"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// stubProxy is a fake ProxyAuth that records the Identity it received and
// returns canned claims or an error.
type stubProxy struct {
	claims     snoozetypes.Claims
	err        error
	gotID      auth.Identity
	gotSignup  bool
	calledOnce bool
}

func (s *stubProxy) Authenticate(_ context.Context, id auth.Identity, autoSignup bool) (snoozetypes.Claims, error) {
	s.gotID = id
	s.gotSignup = autoSignup
	s.calledOnce = true
	return s.claims, s.err
}

func enabledProxyCfg() *schema.AuthProxy {
	c := schema.DefaultAuthProxy()
	c.Enabled = true
	return &c
}

func TestAuth_ProxyHeaderHappyPath(t *testing.T) {
	proxy := &stubProxy{claims: snoozetypes.Claims{
		Subject: "alice", Method: "proxy", TenantID: "default",
		Roles: []string{"operator"}, Permissions: []string{"ro_rule"}, Groups: []string{"ops"},
	}}
	var seen snoozetypes.Claims
	var seenTenant string
	h := AuthWithProxy(nil, nil, proxy, enabledProxyCfg(), nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, ok := auth.ClaimsFrom(r.Context())
		if !ok {
			t.Fatal("claims must be stamped on proxy auth")
		}
		seen = c
		seenTenant, _ = auth.TenantFrom(r.Context())
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/rule", nil)
	req.Header.Set("X-Forwarded-User", "alice")
	req.Header.Set("X-Forwarded-Groups", "ops")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if seen.Subject != "alice" {
		t.Fatalf("claims subject = %q, want alice", seen.Subject)
	}
	if seenTenant != snoozetypes.DefaultTenant {
		t.Fatalf("tenant = %q, want %q", seenTenant, snoozetypes.DefaultTenant)
	}
	// The middleware passed the parsed identity (groups trimmed/split) and the
	// configured auto-signup flag through to the provisioner.
	if proxy.gotID.Username != "alice" || proxy.gotID.Method != "proxy" {
		t.Fatalf("identity = %+v, want alice/proxy", proxy.gotID)
	}
	if len(proxy.gotID.Groups) != 1 || proxy.gotID.Groups[0] != "ops" {
		t.Fatalf("groups = %v, want [ops]", proxy.gotID.Groups)
	}
	if !proxy.gotSignup {
		t.Fatal("auto-signup flag was not forwarded")
	}
}

// TestAuth_ProxyDisabledIsByteIdentical asserts that with the proxy disabled
// (or via the plain Auth constructor) the headers are ignored entirely and the
// request goes through the normal Bearer path — a missing Authorization header
// still 401s.
func TestAuth_ProxyDisabledFallsThrough(t *testing.T) {
	proxy := &stubProxy{}
	cfg := schema.DefaultAuthProxy() // Enabled: false
	h := AuthWithProxy(testEngine(t), nil, proxy, &cfg, nil)(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("downstream must not be reached without auth")
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/rule", nil)
	req.Header.Set("X-Forwarded-User", "alice")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if proxy.calledOnce {
		t.Fatal("proxy provisioner must not be called when disabled")
	}
}

// TestAuth_ProxyGateFallsThroughToBearer: proxy mode on, but the request
// carries a valid Bearer JWT and no user header → the JWT authenticates (proxy
// branch falls through, never blocks).
func TestAuth_ProxyGateFallsThroughToBearer(t *testing.T) {
	eng := testEngine(t)
	tok, _, err := eng.Sign(snoozetypes.Claims{Subject: "bearer-bob", Method: "local", TenantID: "default"})
	if err != nil {
		t.Fatal(err)
	}
	proxy := &stubProxy{}
	var seen snoozetypes.Claims
	h := AuthWithProxy(eng, nil, proxy, enabledProxyCfg(), nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen, _ = auth.ClaimsFrom(r.Context())
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/rule", nil)
	req.Header.Set("Authorization", "Bearer "+tok) // no X-Forwarded-User
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if seen.Subject != "bearer-bob" {
		t.Fatalf("subject = %q, want bearer-bob (JWT must win)", seen.Subject)
	}
	if proxy.calledOnce {
		t.Fatal("proxy provisioner must not run when no user header is present")
	}
}

// TestAuth_ProxyUntrustedIPFallsThrough: TrustedProxies set, the request's
// client IP is outside it, and a user header IS present → headers are ignored,
// and with no Authorization the request 401s (does NOT trust the headers).
func TestAuth_ProxyUntrustedIPFallsThrough(t *testing.T) {
	cfg := enabledProxyCfg()
	cfg.TrustedProxies = []string{"10.0.0.0/8"}
	proxy := &stubProxy{claims: snoozetypes.Claims{Subject: "alice", Method: "proxy"}}
	h := AuthWithProxy(testEngine(t), nil, proxy, cfg, nil)(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("downstream must not be reached for an untrusted-IP proxy request")
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/rule", nil)
	req.Header.Set("X-Forwarded-User", "alice")
	req.RemoteAddr = "203.0.113.7:5555" // outside 10.0.0.0/8
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if proxy.calledOnce {
		t.Fatal("proxy provisioner must not run from an untrusted IP")
	}
}

// TestAuth_ProxyTrustedIPAuthenticates: the same request from inside the
// allowlist authenticates via the proxy branch.
func TestAuth_ProxyTrustedIPAuthenticates(t *testing.T) {
	cfg := enabledProxyCfg()
	cfg.TrustedProxies = []string{"10.0.0.0/8"}
	proxy := &stubProxy{claims: snoozetypes.Claims{Subject: "alice", Method: "proxy", TenantID: "default"}}
	reached := false
	h := AuthWithProxy(testEngine(t), nil, proxy, cfg, nil)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/rule", nil)
	req.Header.Set("X-Forwarded-User", "alice")
	req.RemoteAddr = "10.1.2.3:4444" // inside 10.0.0.0/8
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !reached {
		t.Fatalf("status = %d reached=%v, want 200/true", rec.Code, reached)
	}
	if !proxy.calledOnce {
		t.Fatal("proxy provisioner must run from a trusted IP")
	}
}

// TestAuth_ProxySpoofedXFFDoesNotBypassAllowlist is the security regression for
// CWE-290/CWE-348: an UNtrusted direct TCP peer (203.0.113.7, outside the
// 10.0.0.0/8 allowlist) sends X-Forwarded-For: <a trusted-proxy IP> together
// with X-Forwarded-User: root and no Authorization. The trust gate must compare
// against the genuine peer (PeerIP), NOT the spoofable XFF header (ClientIP), so
// the allowlist rejects, the provisioner is never consulted (no JIT user, no
// ephemeral Claims), and the request falls through to the missing-Authorization
// 401. Built through the production order: CapturePeerIP wraps AuthWithProxy.
func TestAuth_ProxySpoofedXFFDoesNotBypassAllowlist(t *testing.T) {
	cfg := enabledProxyCfg()
	cfg.TrustedProxies = []string{"10.0.0.0/8"}
	proxy := &stubProxy{claims: snoozetypes.Claims{Subject: "root", Method: "proxy"}}
	h := CapturePeerIP(AuthWithProxy(testEngine(t), nil, proxy, cfg, nil)(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("downstream must not be reached for a spoofed-XFF proxy request")
	})))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/rule", nil)
	req.RemoteAddr = "203.0.113.7:5555"           // genuine peer, OUTSIDE 10.0.0.0/8
	req.Header.Set("X-Forwarded-For", "10.0.0.1") // spoofed trusted-proxy IP
	req.Header.Set("X-Forwarded-User", "root")
	// no Authorization header
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (spoofed XFF must not satisfy the allowlist)", rec.Code)
	}
	if proxy.calledOnce {
		t.Fatal("provisioner was consulted on a spoofed XFF — auth bypass NOT closed")
	}
}

// TestAuth_ProxyXRealIPDoesNotBypassAllowlist is the sibling regression covering
// the second header ClientIP/RealIP honor: spoofing via X-Real-IP instead of
// X-Forwarded-For must equally fail the peer-based trust gate.
func TestAuth_ProxyXRealIPDoesNotBypassAllowlist(t *testing.T) {
	cfg := enabledProxyCfg()
	cfg.TrustedProxies = []string{"10.0.0.0/8"}
	proxy := &stubProxy{claims: snoozetypes.Claims{Subject: "root", Method: "proxy"}}
	h := CapturePeerIP(AuthWithProxy(testEngine(t), nil, proxy, cfg, nil)(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("downstream must not be reached for a spoofed-X-Real-IP proxy request")
	})))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/rule", nil)
	req.RemoteAddr = "203.0.113.7:5555"     // genuine peer, OUTSIDE 10.0.0.0/8
	req.Header.Set("X-Real-IP", "10.0.0.1") // spoofed trusted-proxy IP
	req.Header.Set("X-Forwarded-User", "root")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (spoofed X-Real-IP must not satisfy the allowlist)", rec.Code)
	}
	if proxy.calledOnce {
		t.Fatal("provisioner was consulted on a spoofed X-Real-IP — auth bypass NOT closed")
	}
}

// TestAuth_ProxyNoAutoSignup403: the provisioner returns ErrUserNotProvisioned
// → a 403 ErrEnvelope (not a fall-through, not a 401).
func TestAuth_ProxyNoAutoSignup403(t *testing.T) {
	cfg := enabledProxyCfg()
	cfg.AutoSignup = false
	proxy := &stubProxy{err: auth.ErrUserNotProvisioned}
	h := AuthWithProxy(testEngine(t), nil, proxy, cfg, nil)(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("downstream must not be reached for an unprovisioned user")
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/rule", nil)
	req.Header.Set("X-Forwarded-User", "ghost")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	var env snoozetypes.ErrEnvelope
	if err := decodeEnvelope(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error.Code != "forbidden" {
		t.Fatalf("error code = %q, want forbidden", env.Error.Code)
	}
}

// TestAuth_ProxyOtherErrorIs401: any non-ErrUserNotProvisioned provisioner
// error → 401 "proxy auth failed".
func TestAuth_ProxyOtherErrorIs401(t *testing.T) {
	proxy := &stubProxy{err: context.DeadlineExceeded}
	h := AuthWithProxy(testEngine(t), nil, proxy, enabledProxyCfg(), nil)(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("downstream must not be reached on provisioner error")
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/rule", nil)
	req.Header.Set("X-Forwarded-User", "alice")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

type stubKeys struct {
	claims snoozetypes.Claims
	err    error
}

func (s stubKeys) Resolve(_ context.Context, _ string) (snoozetypes.Claims, error) {
	return s.claims, s.err
}

func TestAuth_APIKeyPath(t *testing.T) {
	keys := stubKeys{claims: snoozetypes.Claims{Subject: "alice", Method: auth.APIKeyMethod, TenantID: "default", Permissions: []string{"ro_rule"}}}
	var gotSub string
	h := Auth(nil, keys, nil)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		if c, ok := auth.ClaimsFrom(r.Context()); ok {
			gotSub = c.Subject
		}
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/rule", nil)
	req.Header.Set("Authorization", "Bearer "+auth.APIKeyPrefix+"deadbeef")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if gotSub != "alice" {
		t.Fatalf("api key auth did not stamp claims (sub=%q, status=%d)", gotSub, rec.Code)
	}
}

func TestAuth_APIKeyRejected(t *testing.T) {
	keys := stubKeys{err: auth.ErrAPIKeyExpired}
	h := Auth(nil, keys, nil)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/rule", nil)
	req.Header.Set("Authorization", "Bearer "+auth.APIKeyPrefix+"x")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

// testEngine builds an HS256 token engine for the proxy middleware tests so the
// Bearer fall-through path can verify a real signed token.
func testEngine(t *testing.T) *auth.TokenEngine {
	t.Helper()
	cfg := schema.DefaultAuth()
	cfg.TokenLease = schema.Duration(time.Hour)
	eng, err := auth.NewTokenEngine([]byte("00000000000000000000000000000000"), cfg)
	if err != nil {
		t.Fatalf("token engine: %v", err)
	}
	return eng
}

// decodeEnvelope unmarshals a JSON error envelope from a response body.
func decodeEnvelope(b []byte, env *snoozetypes.ErrEnvelope) error {
	return json.Unmarshal(b, env)
}
