package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

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
	req := httptest.NewRequest("GET", "/api/v1/rule", nil)
	req.Header.Set("Authorization", "Bearer "+auth.APIKeyPrefix+"deadbeef")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if gotSub != "alice" {
		t.Fatalf("api key auth did not stamp claims (sub=%q, status=%d)", gotSub, rec.Code)
	}
}

func TestAuth_APIKeyRejected(t *testing.T) {
	keys := stubKeys{err: auth.ErrAPIKeyExpired}
	h := Auth(nil, keys, nil)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	req := httptest.NewRequest("GET", "/api/v1/rule", nil)
	req.Header.Set("Authorization", "Bearer "+auth.APIKeyPrefix+"x")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}
