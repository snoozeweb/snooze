package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/condition"
)

const whoAmIKeyJSON = `{"name":"alice","method":"ldap","via":"apikey","tenant_id":"default",` +
	`"roles":["ops"],"permissions":["rw_record","rw_protected"],` +
	`"key":{"uid":"k1","name":"laptop","key_prefix":"snz_abcdefgh","expires_at":1893456000}}`

func TestWhoAmI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v1/user/me", r.URL.Path)
		require.Equal(t, "Bearer snz_abcdefgh123", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(whoAmIKeyJSON))
	}))
	defer srv.Close()
	rt, _, _ := newTestRuntime(t, srv)
	rt.flags.Token = "snz_abcdefgh123"

	out, _, err := executeCmd(t, rt, "whoami")
	require.NoError(t, err)
	require.Contains(t, out, `alice (ldap) via apikey "laptop" [snz_abcdefgh…], expires 2030-01-01T00:00:00Z, tenant default`)
	require.Contains(t, out, "permissions: rw_record, rw_protected")
}

// With an API key, --owner me is the key's owner even when client.yaml also
// names a (shared) username.
func TestRecordListOwnerMeFromAPIKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/user/me" {
			_, _ = w.Write([]byte(whoAmIKeyJSON))
			return
		}
		require.Equal(t, condition.Equals("owner", "alice"), listQ(t, r))
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()
	rt, _, _ := newTestRuntime(t, srv)
	rt.flags.Token = "snz_abcdefgh123"
	rt.flags.User = "snooze"

	_, _, err := executeCmd(t, rt, "record", "list", "--owner", "me")
	require.NoError(t, err)
}

func TestAPIKeyCreate(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/api/v1/user/me/apikeys", r.URL.Path)
		require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"uid":"k9","name":"laptop","key":"snz_secret","expires_at":1893456000}`))
	}))
	defer srv.Close()
	rt, _, _ := newTestRuntime(t, srv)
	rt.flags.Token = "session.jwt.token"

	out, _, err := executeCmd(t, rt, "apikey", "create", "--name", "laptop",
		"--perm", "rw_record,rw_protected", "--expires", "2029-06-01T00:00:00Z")
	require.NoError(t, err)
	require.Equal(t, "laptop", got["name"])
	require.Equal(t, []any{"rw_record", "rw_protected"}, got["permissions"])
	require.Equal(t, "2029-06-01T00:00:00Z", got["expires_at"])
	require.Contains(t, out, "snz_secret")
	require.Contains(t, out, "credentials:\n    token: snz_secret")
	require.Contains(t, out, "export SNOOZE_TOKEN=snz_secret")
}

// No --perm is an error naming what the caller could grant — never an
// implicit "everything".
func TestAPIKeyCreateRequiresPerm(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v1/user/me", r.URL.Path, "must not mint")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"alice","method":"local","via":"session","roles":[],"permissions":["rw_record","ro_rule"]}`))
	}))
	defer srv.Close()
	rt, _, _ := newTestRuntime(t, srv)
	rt.flags.Token = "session.jwt.token"

	_, _, err := executeCmd(t, rt, "apikey", "create", "--name", "x")
	require.ErrorContains(t, err, "--perm is required")
	require.ErrorContains(t, err, "rw_record, ro_rule")
}

func TestAPIKeyCreateRefusesWhenUsingAKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		t.Fatalf("server must not be called, got %s", r.URL)
	}))
	defer srv.Close()
	rt, _, _ := newTestRuntime(t, srv)
	rt.flags.Token = "snz_abc"

	_, _, err := executeCmd(t, rt, "apikey", "create", "--name", "x", "--perm", "ro_rule")
	require.ErrorContains(t, err, "a key cannot mint another")
	require.ErrorContains(t, err, "--token=")
}

func TestAPIKeyListAndRevoke(t *testing.T) {
	var deleted string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			require.Equal(t, "/api/v1/user/me/apikeys", r.URL.Path)
			_, _ = w.Write([]byte(`{"data":[{"uid":"k1","name":"laptop","key_prefix":"snz_abcdefgh",` +
				`"permissions":["rw_record"],"expires_at":1893456000}]}`))
		case http.MethodDelete:
			deleted = r.URL.Path
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer srv.Close()
	rt, _, _ := newTestRuntime(t, srv)
	rt.flags.Token = "snz_abc"

	out, _, err := executeCmd(t, rt, "apikey", "list")
	require.NoError(t, err)
	require.Regexp(t, `k1\s+laptop\s+snz_abcdefgh…\s+rw_record\s+2030-01-01T00:00:00Z\s+never`, out)

	out, _, err = executeCmd(t, rt, "apikey", "revoke", "k1")
	require.NoError(t, err)
	require.Equal(t, "/api/v1/user/me/apikeys/k1", deleted)
	require.Contains(t, out, "Revoked API key k1")
}

func TestParseExpiry(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	for in, want := range map[string]time.Time{
		"90d":                  now.Add(90 * 24 * time.Hour),
		"720h":                 now.Add(720 * time.Hour),
		"2027-01-01T00:00:00Z": time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
	} {
		got, err := parseExpiry(in, now)
		require.NoError(t, err, in)
		require.True(t, want.Equal(got), "%s: got %s", in, got)
	}
	for _, bad := range []string{"soon", "0d", "-5h", "d"} {
		_, err := parseExpiry(bad, now)
		require.Error(t, err, bad)
	}
}
