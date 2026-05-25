package teams

import (
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

// newDelegatedTestGraph wires a graphClient in delegated mode pointed at a
// httptest server, plus a pre-populated token store. Returns the client and
// the token file path so tests can inspect / mutate it.
func newDelegatedTestGraph(t *testing.T, h http.HandlerFunc, initial cachedToken) (*graphClient, string, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	path := filepath.Join(t.TempDir(), "token.json")
	// Seed the store so the client has something to refresh from.
	if initial.RefreshToken != "" {
		require.NoError(t, newTokenStore(path).Save(initial))
	}
	cfg := Config{
		TenantID:       "tenant",
		ClientID:       "client",
		AuthMode:       "delegated",
		TokenFile:      path,
		Scopes:         []string{"ChannelMessage.Send", "offline_access"},
		LoginBase:      srv.URL,
		GraphBase:      srv.URL + "/v1.0",
		RequestTimeout: 2 * time.Second,
	}
	return newGraphClient(cfg, srv.Client()), path, srv
}

func TestDelegated_UsesCachedAccessTokenWhileFresh(t *testing.T) {
	var tokenHits atomic.Int32
	g, _, _ := newDelegatedTestGraph(t, func(w http.ResponseWriter, r *http.Request) {
		require.False(t, strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token"),
			"token endpoint should not be called when cached access token is fresh")
		tokenHits.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}, cachedToken{
		AccessToken:  "fresh-AT",
		RefreshToken: "RT",
		ExpiresAt:    time.Now().Add(30 * time.Minute), // well above graphTokenSkew
	})

	tok, err := g.bearerToken(context.Background())
	require.NoError(t, err)
	require.Equal(t, "fresh-AT", tok)
	require.Equal(t, int32(0), tokenHits.Load())
}

func TestDelegated_RefreshesExpiredAccessToken(t *testing.T) {
	var refreshCalls atomic.Int32
	g, path, _ := newDelegatedTestGraph(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token") {
			refreshCalls.Add(1)
			require.NoError(t, r.ParseForm())
			require.Equal(t, "refresh_token", r.PostForm.Get("grant_type"))
			require.Equal(t, "RT-old", r.PostForm.Get("refresh_token"))
			require.Equal(t, "client", r.PostForm.Get("client_id"))
			require.Equal(t, "ChannelMessage.Send offline_access", r.PostForm.Get("scope"))
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"token_type":    "Bearer",
				"expires_in":    3600,
				"access_token":  "AT-new",
				"refresh_token": "RT-new", // AAD rotated
				"scope":         "ChannelMessage.Send offline_access",
			})
			return
		}
		http.NotFound(w, r)
	}, cachedToken{
		AccessToken:  "AT-stale",
		RefreshToken: "RT-old",
		ExpiresAt:    time.Now().Add(-time.Minute), // already expired
	})

	tok, err := g.bearerToken(context.Background())
	require.NoError(t, err)
	require.Equal(t, "AT-new", tok)
	require.Equal(t, int32(1), refreshCalls.Load())

	// Disk should reflect the rotated refresh token so the next daemon
	// restart picks up where we left off.
	got, err := newTokenStore(path).Load()
	require.NoError(t, err)
	require.Equal(t, "AT-new", got.AccessToken)
	require.Equal(t, "RT-new", got.RefreshToken)
}

func TestDelegated_KeepsOldRefreshTokenWhenAADDoesntRotate(t *testing.T) {
	g, path, _ := newDelegatedTestGraph(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token") {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "AT-new",
				"expires_in":   3600,
				// refresh_token intentionally omitted
			})
			return
		}
		http.NotFound(w, r)
	}, cachedToken{
		AccessToken:  "AT-stale",
		RefreshToken: "RT-keep",
		ExpiresAt:    time.Now().Add(-time.Minute),
	})

	_, err := g.bearerToken(context.Background())
	require.NoError(t, err)

	got, err := newTokenStore(path).Load()
	require.NoError(t, err)
	require.Equal(t, "RT-keep", got.RefreshToken, "refresh token must survive when AAD doesn't rotate it")
}

func TestDelegated_MissingTokenFileSurfacesAuthorizeHint(t *testing.T) {
	cfg := Config{
		TenantID:       "tenant",
		ClientID:       "client",
		AuthMode:       "delegated",
		TokenFile:      filepath.Join(t.TempDir(), "missing.json"),
		Scopes:         []string{"ChannelMessage.Send"},
		LoginBase:      "http://invalid.invalid",
		GraphBase:      "http://invalid.invalid/v1.0",
		RequestTimeout: time.Second,
	}
	g := newGraphClient(cfg, &http.Client{Timeout: time.Second})
	_, err := g.bearerToken(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "snooze-teams authorize")
}

func TestDelegated_AADRejectionMentionsAuthorize(t *testing.T) {
	g, _, _ := newDelegatedTestGraph(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error":             "invalid_grant",
			"error_description": "AADSTS70008: refresh token expired.",
		})
	}, cachedToken{
		AccessToken:  "AT",
		RefreshToken: "RT",
		ExpiresAt:    time.Now().Add(-time.Minute),
	})

	_, err := g.bearerToken(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "snooze-teams authorize")
	require.Contains(t, err.Error(), "invalid_grant")
}
