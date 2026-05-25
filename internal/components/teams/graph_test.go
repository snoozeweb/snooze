package teams

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// newTestGraph spins up an httptest.Server, points a graphClient at it, and
// returns both. The handler is supplied by the caller — it must distinguish
// /tenant/oauth2/v2.0/token from the Graph endpoints.
func newTestGraph(t *testing.T, h http.HandlerFunc) (*graphClient, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	cfg := Config{
		TenantID:       "tenant",
		ClientID:       "client",
		ClientSecret:   "secret",
		Scope:          "https://graph.microsoft.com/.default",
		LoginBase:      srv.URL,
		GraphBase:      srv.URL + "/v1.0",
		RequestTimeout: 2 * time.Second,
	}
	return newGraphClient(cfg, srv.Client()), srv
}

func TestFetchToken(t *testing.T) {
	g, _ := newTestGraph(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/tenant/oauth2/v2.0/token", r.URL.Path)
		require.NoError(t, r.ParseForm())
		require.Equal(t, "client_credentials", r.PostForm.Get("grant_type"))
		require.Equal(t, "client", r.PostForm.Get("client_id"))
		require.Equal(t, "secret", r.PostForm.Get("client_secret"))
		require.Equal(t, "https://graph.microsoft.com/.default", r.PostForm.Get("scope"))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"token_type":   "Bearer",
			"expires_in":   3600,
			"access_token": "tok-xyz",
		})
	})
	tok, err := g.fetchToken(context.Background())
	require.NoError(t, err)
	require.Equal(t, "tok-xyz", tok)

	// Second call should hit the cache, not the wire.
	tok2, err := g.bearerToken(context.Background())
	require.NoError(t, err)
	require.Equal(t, "tok-xyz", tok2)
}

func TestFetchToken_errorResponse(t *testing.T) {
	g, _ := newTestGraph(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error":             "invalid_client",
			"error_description": "AADSTS7000215: Invalid client secret.",
		})
	})
	_, err := g.fetchToken(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid_client")
}

func TestSendMessage(t *testing.T) {
	var posted atomic.Pointer[map[string]any]
	g, _ := newTestGraph(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "tok", "expires_in": 3600,
			})
		case strings.Contains(r.URL.Path, "/messages"):
			require.Equal(t, http.MethodPost, r.Method)
			require.Equal(t, "Bearer tok", r.Header.Get("Authorization"))
			var body map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			posted.Store(&body)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":              "1700000000000",
				"createdDateTime": time.Now().UTC().Format(time.RFC3339),
				"body":            map[string]any{"contentType": "html", "content": "<p>hi</p>"},
			})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	})
	out, err := g.sendMessage(context.Background(), "team", "channel", "<b>hello</b>", sendOpts{})
	require.NoError(t, err)
	require.Equal(t, "1700000000000", out.ID)
	require.NotNil(t, posted.Load())
	body := *posted.Load()
	require.Equal(t, map[string]any{"contentType": "html", "content": "<b>hello</b>"}, body["body"])
}

func TestFetchMessages(t *testing.T) {
	g, _ := newTestGraph(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "tok", "expires_in": 3600,
			})
		case strings.Contains(r.URL.Path, "/messages"):
			require.Equal(t, http.MethodGet, r.Method)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"value": []map[string]any{
					{
						"id":   "msg-1",
						"body": map[string]any{"contentType": "text", "content": "hello"},
						"from": map[string]any{"user": map[string]any{"id": "u1", "displayName": "Alice"}},
					},
				},
			})
		}
	})
	msgs, err := g.fetchMessages(context.Background(), "team", "channel")
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	require.Equal(t, "msg-1", msgs[0].ID)
	require.Equal(t, "Alice", msgs[0].From.User.DisplayName)
}

func TestDoJSON_retriesOn401(t *testing.T) {
	var tokenHits, msgHits int32
	g, _ := newTestGraph(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token"):
			n := atomic.AddInt32(&tokenHits, 1)
			tok := "tok-first"
			if n > 1 {
				tok = "tok-second"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": tok, "expires_in": 3600,
			})
		case strings.Contains(r.URL.Path, "/messages"):
			n := atomic.AddInt32(&msgHits, 1)
			if n == 1 {
				// First call: pretend the cached token is stale.
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
				return
			}
			require.Equal(t, "Bearer tok-second", r.Header.Get("Authorization"))
			_ = json.NewEncoder(w).Encode(map[string]any{"value": []any{}})
		}
	})
	_, err := g.fetchMessages(context.Background(), "team", "channel")
	require.NoError(t, err)
	require.EqualValues(t, 2, atomic.LoadInt32(&tokenHits), "expected token refresh after 401")
	require.EqualValues(t, 2, atomic.LoadInt32(&msgHits), "expected message endpoint to be retried")
}

func TestDoJSON_non2xxBubbles(t *testing.T) {
	g, _ := newTestGraph(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "tok", "expires_in": 3600,
			})
		default:
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":{"code":"Forbidden","message":"go away"}}`))
		}
	})
	_, err := g.fetchMessages(context.Background(), "team", "channel")
	require.Error(t, err)
	require.Contains(t, err.Error(), "403")
	require.Contains(t, err.Error(), "Forbidden")
}

func TestSendMessage_ReplyHitsRepliesEndpoint(t *testing.T) {
	var (
		seenPath atomic.Pointer[string]
		posted   atomic.Pointer[map[string]any]
	)
	g, _ := newTestGraph(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "tok", "expires_in": 3600,
			})
		case strings.Contains(r.URL.Path, "/replies"):
			require.Equal(t, http.MethodPost, r.Method)
			p := r.URL.Path
			seenPath.Store(&p)
			var body map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			posted.Store(&body)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":        "1700000000099",
				"replyToId": "1700000000000",
				"body":      map[string]any{"contentType": "html", "content": "<p>reply</p>"},
			})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	})
	out, err := g.sendMessage(context.Background(), "team", "channel", "<b>reply</b>",
		sendOpts{ReplyToID: "1700000000000"})
	require.NoError(t, err)
	require.Equal(t, "1700000000099", out.ID)
	require.NotNil(t, seenPath.Load())
	// The replies endpoint shape: /teams/<t>/channels/<c>/messages/<root>/replies
	require.Contains(t, *seenPath.Load(), "/messages/1700000000000/replies")
}

// TestFetchDelegatedToken_PublicClientSuppressesSecret asserts the bridge
// honours the public_client flag: the refresh-token request must NOT carry
// a client_secret even when teams.yaml still has one configured.
func TestFetchDelegatedToken_PublicClientSuppressesSecret(t *testing.T) {
	dir := t.TempDir()
	tokenPath := dir + "/teams-token.json"
	store := newTokenStore(tokenPath)
	require.NoError(t, store.Save(cachedToken{
		RefreshToken: "rt-existing",
		ExpiresAt:    time.Now().Add(-time.Hour), // expired → forces a refresh
		TenantID:     "tenant",
		ClientID:     "client",
	}))

	var posted atomic.Pointer[string]
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/tenant/oauth2/v2.0/token", r.URL.Path)
		raw, _ := r.GetBody, error(nil)
		_ = raw
		require.NoError(t, r.ParseForm())
		form := r.PostForm.Encode()
		posted.Store(&form)
		require.Equal(t, "refresh_token", r.PostForm.Get("grant_type"))
		require.Equal(t, "rt-existing", r.PostForm.Get("refresh_token"))
		require.Empty(t, r.PostForm.Get("client_secret"),
			"public client must not send client_secret on refresh")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"token_type":    "Bearer",
			"expires_in":    3600,
			"access_token":  "tok-new",
			"refresh_token": "rt-rotated",
		})
	}))
	t.Cleanup(srv.Close)

	g := &graphClient{
		httpc:        srv.Client(),
		mode:         authModeDelegated,
		tenantID:     "tenant",
		clientID:     "client",
		clientSecret: "leftover-secret", // present in yaml, must be suppressed
		publicClient: true,              // the new flag
		scope:        "ChannelMessage.Send offline_access",
		store:        store,
		loginBase:    srv.URL,
	}
	tok, err := g.fetchToken(context.Background())
	require.NoError(t, err)
	require.Equal(t, "tok-new", tok)
	require.NotNil(t, posted.Load())
	require.NotContains(t, *posted.Load(), "client_secret=")
}

// TestRefreshHint_PublicClientMistake asserts AADSTS700025 is translated
// into a useful operator hint instead of the old "run `snooze-teams
// authorize`" message that pointed at the wrong remedy.
func TestRefreshHint_PublicClientMistake(t *testing.T) {
	dir := t.TempDir()
	store := newTokenStore(dir + "/token.json")
	require.NoError(t, store.Save(cachedToken{
		RefreshToken: "rt-x",
		ExpiresAt:    time.Now().Add(-time.Hour),
	}))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error":             "invalid_client",
			"error_description": "AADSTS700025: Client is public so neither 'client_assertion' nor 'client_secret' should be presented.",
		})
	}))
	t.Cleanup(srv.Close)

	g := &graphClient{
		httpc:        srv.Client(),
		mode:         authModeDelegated,
		tenantID:     "tenant",
		clientID:     "client",
		clientSecret: "wrong-to-send",
		// Intentionally NOT setting publicClient — we want to see what the
		// hint message looks like when the operator hasn't migrated yet.
		store:     store,
		loginBase: srv.URL,
	}
	_, err := g.fetchToken(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "public_client: true",
		"the hint must point at the config knob that fixes AADSTS700025")
	require.NotContains(t, err.Error(), "snooze-teams authorize",
		"the old 'run authorize' suggestion was misleading for this code")
}
