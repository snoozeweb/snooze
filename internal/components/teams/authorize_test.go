package teams

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// newAuthorizeTestServer wires a server that:
//
//  1. Returns a deterministic devicecode response.
//  2. On `/oauth2/v2.0/token` returns authorization_pending for the first N
//     calls (configurable), then the final token response.
//
// The interval is set deliberately low (1s) so the test completes quickly.
func newAuthorizeTestServer(t *testing.T, pendingHits int, tokenResp map[string]any) (string, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/devicecode"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"device_code":      "DEVICE",
				"user_code":        "USER-CODE",
				"verification_uri": "https://aka.ms/devicelogin",
				"interval":         1,
				"expires_in":       30,
				"message":          "Go to https://aka.ms/devicelogin and enter USER-CODE.",
			})
		case strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token"):
			n := int(hits.Add(1))
			if n <= pendingHits {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error":             "authorization_pending",
					"error_description": "waiting on user",
				})
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(tokenResp)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL, &hits
}

func TestAuthorize_HappyPath(t *testing.T) {
	url, _ := newAuthorizeTestServer(t, 1, map[string]any{
		"access_token":  "AT-ok",
		"refresh_token": "RT-ok",
		"expires_in":    3600,
		"scope":         "ChannelMessage.Send offline_access",
		"token_type":    "Bearer",
	})
	tokenFile := filepath.Join(t.TempDir(), "token.json")
	cfg := Config{
		TenantID:       "tenant",
		ClientID:       "client",
		AuthMode:       "delegated",
		TokenFile:      tokenFile,
		Scopes:         []string{"ChannelMessage.Send", "offline_access"},
		LoginBase:      url,
		RequestTimeout: 3 * time.Second,
	}
	var out bytes.Buffer

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	require.NoError(t, Authorize(ctx, cfg, &out))
	require.Contains(t, out.String(), "USER-CODE")
	require.Contains(t, out.String(), "Authorization complete")

	got, err := newTokenStore(tokenFile).Load()
	require.NoError(t, err)
	require.Equal(t, "AT-ok", got.AccessToken)
	require.Equal(t, "RT-ok", got.RefreshToken)
	require.Equal(t, "tenant", got.TenantID)
}

func TestAuthorize_RejectsMissingOfflineAccess(t *testing.T) {
	// AAD will sometimes return a token without refresh_token if
	// offline_access was not requested. We treat that as a hard error so
	// operators don't end up with a daemon that can post once and then
	// silently breaks an hour later.
	url, _ := newAuthorizeTestServer(t, 0, map[string]any{
		"access_token": "AT",
		"expires_in":   3600,
		// no refresh_token
	})
	cfg := Config{
		TenantID:       "tenant",
		ClientID:       "client",
		AuthMode:       "delegated",
		TokenFile:      filepath.Join(t.TempDir(), "token.json"),
		Scopes:         []string{"ChannelMessage.Send"},
		LoginBase:      url,
		RequestTimeout: 3 * time.Second,
	}
	err := Authorize(context.Background(), cfg, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "offline_access")
}

func TestAuthorize_AccessDeniedSurfacesClearly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/devicecode") {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"device_code":      "DEVICE",
				"user_code":        "U",
				"verification_uri": "https://aka.ms/devicelogin",
				"interval":         1,
				"expires_in":       30,
			})
			return
		}
		if strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error":             "access_denied",
				"error_description": "user declined",
			})
		}
	}))
	t.Cleanup(srv.Close)
	cfg := Config{
		TenantID:       "tenant",
		ClientID:       "client",
		AuthMode:       "delegated",
		TokenFile:      filepath.Join(t.TempDir(), "token.json"),
		Scopes:         []string{"ChannelMessage.Send"},
		LoginBase:      srv.URL,
		RequestTimeout: 3 * time.Second,
	}
	err := Authorize(context.Background(), cfg, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "denied")
}

func TestAuthorize_RefusesAppOnlyMode(t *testing.T) {
	cfg := Config{
		TenantID:  "tenant",
		ClientID:  "client",
		AuthMode:  "client_credentials",
		TokenFile: filepath.Join(t.TempDir(), "token.json"),
		Scopes:    []string{"ChannelMessage.Send"},
		LoginBase: "http://localhost.invalid",
	}
	err := Authorize(context.Background(), cfg, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "auth_mode=delegated")
}
