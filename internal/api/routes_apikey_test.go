package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/config/schema"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/db/sqlite"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// newAPIKeyTestRouter mounts /api/v1/user/me/* against a fresh sqlite driver
// pre-populated with a single local user "alice" and a role "r1" granting
// {rw_record, ro_rule}. The Router carries a real APIKeyStore so the
// self-service handlers can mint/list/delete keys. The chi router is bare — the
// caller injects auth.Claims via withClaims (defined in routes_user_test.go) to
// simulate the global Auth middleware, exactly as the password-change tests do.
func newAPIKeyTestRouter(t *testing.T) (chi.Router, *Router) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "snooze.db")
	drv, err := sqlite.New(context.Background(), sqlite.Config{Path: path})
	require.NoError(t, err)
	t.Cleanup(func() { _ = drv.Close() })

	tctx := snoozetypes.WithTenant(context.Background(), snoozetypes.DefaultTenant)
	_, err = drv.Write(tctx, auth.RoleCollection, []db.Document{{
		"name": "r1", "permissions": []any{"rw_record", "ro_rule"},
	}}, db.WriteOptions{Primary: []string{"tenant_id", "name"}, UpdateTime: true})
	require.NoError(t, err)
	_, err = drv.Write(tctx, auth.LocalCollection, []db.Document{{
		"name": "alice", "method": auth.LocalMethod, "enabled": true,
		"roles": []any{"r1"},
	}}, db.WriteOptions{Primary: []string{"tenant_id", "name", "method"}, UpdateTime: true})
	require.NoError(t, err)

	rt := &Router{DB: drv, APIKeys: auth.NewAPIKeyStore(drv, schema.Duration(time.Hour).AsDuration())}
	r := chi.NewRouter()
	rt.mountUser(r)
	return r, rt
}

// aliceClaims is the live claim set the Auth middleware would stamp for alice
// after resolving her role: a local session carrying {rw_record, ro_rule}.
func aliceClaims() snoozetypes.Claims {
	return snoozetypes.Claims{
		Subject:     "alice",
		Method:      auth.LocalMethod,
		TenantID:    snoozetypes.DefaultTenant,
		Permissions: []string{"rw_record", "ro_rule"},
	}
}

// TestSelfAPIKeys_MintListDelete drives the full lifecycle: POST returns a snz_
// key once; GET lists it without key_hash; DELETE removes it; a second GET is
// empty.
func TestSelfAPIKeys_MintListDelete(t *testing.T) {
	r, _ := newAPIKeyTestRouter(t)

	// Mint.
	mintReq := withClaims(
		httptest.NewRequest(http.MethodPost, "/api/v1/user/me/apikeys",
			bytes.NewBufferString(`{"name":"ci","permissions":["ro_rule"]}`)),
		aliceClaims(),
	)
	mintReq.Header.Set("Content-Type", "application/json")
	mintRec := httptest.NewRecorder()
	r.ServeHTTP(mintRec, mintReq)
	require.Equal(t, http.StatusCreated, mintRec.Code, "body=%s", mintRec.Body.String())

	var created map[string]any
	require.NoError(t, json.Unmarshal(mintRec.Body.Bytes(), &created))
	rawKey, _ := created["key"].(string)
	require.NotEmpty(t, rawKey, "create response must carry the raw key once")
	require.True(t, len(rawKey) > len(auth.APIKeyPrefix) && rawKey[:len(auth.APIKeyPrefix)] == auth.APIKeyPrefix,
		"raw key must be snz_-prefixed, got %q", rawKey)
	require.Nil(t, created["key_hash"], "create response must not leak key_hash")

	// List → exactly one key, no key_hash.
	listReq := withClaims(httptest.NewRequest(http.MethodGet, "/api/v1/user/me/apikeys", nil), aliceClaims())
	listRec := httptest.NewRecorder()
	r.ServeHTTP(listRec, listReq)
	require.Equal(t, http.StatusOK, listRec.Code, "body=%s", listRec.Body.String())

	var listed struct {
		Data []map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(listRec.Body.Bytes(), &listed))
	require.Len(t, listed.Data, 1)
	require.Equal(t, "ci", listed.Data[0]["name"])
	require.Nil(t, listed.Data[0]["key_hash"], "list response must not include key_hash")
	require.Nil(t, listed.Data[0]["key"], "list response must not include the raw key")
	uid, _ := listed.Data[0]["uid"].(string)
	require.NotEmpty(t, uid)

	// Delete.
	delReq := withClaims(httptest.NewRequest(http.MethodDelete, "/api/v1/user/me/apikeys/"+uid, nil), aliceClaims())
	delRec := httptest.NewRecorder()
	r.ServeHTTP(delRec, delReq)
	require.Equal(t, http.StatusNoContent, delRec.Code, "body=%s", delRec.Body.String())

	// List again → empty.
	listReq2 := withClaims(httptest.NewRequest(http.MethodGet, "/api/v1/user/me/apikeys", nil), aliceClaims())
	listRec2 := httptest.NewRecorder()
	r.ServeHTTP(listRec2, listReq2)
	require.Equal(t, http.StatusOK, listRec2.Code)
	var listed2 struct {
		Data []map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(listRec2.Body.Bytes(), &listed2))
	require.Empty(t, listed2.Data, "key must be gone after delete")
}

// TestSelfAPIKeys_DeleteNotOwned: deleting an unknown / non-owned id returns 404.
func TestSelfAPIKeys_DeleteNotOwned(t *testing.T) {
	r, _ := newAPIKeyTestRouter(t)
	delReq := withClaims(httptest.NewRequest(http.MethodDelete, "/api/v1/user/me/apikeys/does-not-exist", nil), aliceClaims())
	delRec := httptest.NewRecorder()
	r.ServeHTTP(delRec, delReq)
	require.Equal(t, http.StatusNotFound, delRec.Code, "body=%s", delRec.Body.String())
}

// TestSelfAPIKeys_NoEscalation: minting with permissions exceeding the caller's
// own is refused with 403.
func TestSelfAPIKeys_NoEscalation(t *testing.T) {
	r, _ := newAPIKeyTestRouter(t)
	req := withClaims(
		httptest.NewRequest(http.MethodPost, "/api/v1/user/me/apikeys",
			bytes.NewBufferString(`{"name":"bad","permissions":["rw_tenant"]}`)),
		aliceClaims(),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code, "body=%s", rec.Body.String())

	var env snoozetypes.ErrEnvelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	require.Equal(t, "forbidden", env.Error.Code)
}

// TestSelfAPIKeys_KeyCannotMint: a request that is itself authenticated with an
// API key (claims.Method == auth.APIKeyMethod) is refused (403) from minting
// further keys.
func TestSelfAPIKeys_KeyCannotMint(t *testing.T) {
	r, _ := newAPIKeyTestRouter(t)
	claims := aliceClaims()
	claims.Method = auth.APIKeyMethod
	req := withClaims(
		httptest.NewRequest(http.MethodPost, "/api/v1/user/me/apikeys",
			bytes.NewBufferString(`{"name":"nested","permissions":["ro_rule"]}`)),
		claims,
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code, "body=%s", rec.Body.String())
}
