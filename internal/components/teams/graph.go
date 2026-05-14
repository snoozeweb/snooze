package teams

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// graphTokenSkew is subtracted from the reported token lifetime so we never
// hand out a token that the server might consider already expired by the
// time it lands.
const graphTokenSkew = 60 * time.Second

// graphClient is a tiny Microsoft Graph wrapper. It handles:
//
//   - OAuth2 client_credentials token acquisition (with caching + auto-refresh).
//   - POST /teams/{team}/channels/{channel}/messages.
//   - GET  /teams/{team}/channels/{channel}/messages (paged "value" envelope).
//
// It is intentionally narrow: no msgraph-sdk-go dependency, only stdlib.
type graphClient struct {
	httpc *http.Client

	tenantID     string
	clientID     string
	clientSecret string
	scope        string

	loginBase string
	graphBase string

	mu        sync.Mutex
	token     string
	expiresAt time.Time
}

// newGraphClient builds a graph client from Config. It does not perform any
// network I/O; the first FetchToken / API call triggers the OAuth2 round trip.
func newGraphClient(cfg Config, httpc *http.Client) *graphClient {
	if httpc == nil {
		httpc = &http.Client{Timeout: cfg.RequestTimeout}
	}
	return &graphClient{
		httpc:        httpc,
		tenantID:     cfg.TenantID,
		clientID:     cfg.ClientID,
		clientSecret: cfg.ClientSecret,
		scope:        cfg.Scope,
		loginBase:    cfg.LoginBase,
		graphBase:    cfg.GraphBase,
	}
}

// graphTokenResponse mirrors the wire shape of the v2 token endpoint.
type graphTokenResponse struct {
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
	AccessToken string `json:"access_token"`
}

// graphTokenError mirrors the AAD error envelope: an opaque code plus a
// long human-readable description.
type graphTokenError struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

// fetchToken hits https://login.microsoftonline.com/{tenant}/oauth2/v2.0/token
// with the client_credentials grant and stores the result in memory. It is
// safe to call concurrently — callers normally use bearerToken which lazily
// refreshes on first call and within `graphTokenSkew` of expiry.
func (g *graphClient) fetchToken(ctx context.Context) (string, error) {
	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", g.clientID)
	form.Set("client_secret", g.clientSecret)
	form.Set("scope", g.scope)

	endpoint := fmt.Sprintf("%s/%s/oauth2/v2.0/token", g.loginBase, g.tenantID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("teams: build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := g.httpc.Do(req)
	if err != nil {
		return "", fmt.Errorf("teams: token request: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("teams: read token response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var apiErr graphTokenError
		if jerr := json.Unmarshal(raw, &apiErr); jerr == nil && apiErr.Error != "" {
			return "", fmt.Errorf("teams: token %d %s: %s", resp.StatusCode, apiErr.Error, apiErr.ErrorDescription)
		}
		return "", fmt.Errorf("teams: token %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var tr graphTokenResponse
	if err := json.Unmarshal(raw, &tr); err != nil {
		return "", fmt.Errorf("teams: decode token response: %w", err)
	}
	if tr.AccessToken == "" {
		return "", errors.New("teams: token response missing access_token")
	}
	ttl := time.Duration(tr.ExpiresIn) * time.Second
	if ttl <= 0 {
		ttl = time.Hour
	}
	g.mu.Lock()
	g.token = tr.AccessToken
	g.expiresAt = time.Now().Add(ttl - graphTokenSkew)
	g.mu.Unlock()
	return tr.AccessToken, nil
}

// bearerToken returns a non-expired token, refreshing it transparently when
// the cached one is past expiresAt - graphTokenSkew.
func (g *graphClient) bearerToken(ctx context.Context) (string, error) {
	g.mu.Lock()
	if g.token != "" && time.Now().Before(g.expiresAt) {
		t := g.token
		g.mu.Unlock()
		return t, nil
	}
	g.mu.Unlock()
	return g.fetchToken(ctx)
}

// invalidateToken forces the next bearerToken call to round-trip. Used when
// a 401 from Graph suggests the cached token is stale despite our clock-skew
// safety margin (e.g. tenant admin rotated the secret).
func (g *graphClient) invalidateToken() {
	g.mu.Lock()
	g.token = ""
	g.expiresAt = time.Time{}
	g.mu.Unlock()
}

// graphMessage is the slice of /messages we care about: identity, body,
// timestamp and reply pointer. The Graph payload has many more fields; they
// land in a json.RawMessage on the way out so callers that want them can
// re-parse.
type graphMessage struct {
	ID              string             `json:"id"`
	ReplyToID       string             `json:"replyToId,omitempty"`
	CreatedDateTime time.Time          `json:"createdDateTime,omitempty"`
	From            graphFrom          `json:"from"`
	Body            graphBody          `json:"body"`
	Mentions        []graphMention     `json:"mentions,omitempty"`
	Raw             json.RawMessage    `json:"-"`
	Extra           map[string]any     `json:"-"`
}

type graphFrom struct {
	User        *graphUser        `json:"user,omitempty"`
	Application *graphApplication `json:"application,omitempty"`
}

type graphUser struct {
	ID          string `json:"id,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
}

type graphApplication struct {
	ID          string `json:"id,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
}

type graphBody struct {
	ContentType string `json:"contentType,omitempty"`
	Content     string `json:"content,omitempty"`
}

type graphMention struct {
	ID            int            `json:"id"`
	MentionText   string         `json:"mentionText"`
	Mentioned     map[string]any `json:"mentioned,omitempty"`
}

// graphMessagesPage is the {"value": [...]} envelope returned by GET messages.
type graphMessagesPage struct {
	Value []graphMessage `json:"value"`
}

// sendMessage POSTs a chatMessage to the configured channel. body is rendered
// as HTML and posted under /v1.0/teams/{team}/channels/{channel}/messages.
func (g *graphClient) sendMessage(ctx context.Context, teamID, channelID, htmlBody string) (graphMessage, error) {
	endpoint := fmt.Sprintf("%s/teams/%s/channels/%s/messages", g.graphBase, teamID, channelID)
	payload := map[string]any{
		"body": map[string]any{
			"contentType": "html",
			"content":     htmlBody,
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return graphMessage{}, fmt.Errorf("teams: marshal message: %w", err)
	}
	var out graphMessage
	if err := g.doJSON(ctx, http.MethodPost, endpoint, bytes.NewReader(raw), &out); err != nil {
		return graphMessage{}, err
	}
	return out, nil
}

// fetchMessages returns the top-level channel messages, newest first (as the
// Graph API does by default).
func (g *graphClient) fetchMessages(ctx context.Context, teamID, channelID string) ([]graphMessage, error) {
	endpoint := fmt.Sprintf("%s/teams/%s/channels/%s/messages", g.graphBase, teamID, channelID)
	var page graphMessagesPage
	if err := g.doJSON(ctx, http.MethodGet, endpoint, nil, &page); err != nil {
		return nil, err
	}
	return page.Value, nil
}

// doJSON is the shared transport for the two Graph calls we need: it signs
// the request with a fresh bearer token, retries once on 401 (refreshing the
// token), and decodes the JSON body into dest when non-nil.
func (g *graphClient) doJSON(ctx context.Context, method, endpoint string, body io.Reader, dest any) error {
	// We may need to replay the request after a 401; buffer the body up
	// front so the io.Reader is re-readable on the retry.
	var raw []byte
	if body != nil {
		var err error
		raw, err = io.ReadAll(body)
		if err != nil {
			return fmt.Errorf("teams: buffer body: %w", err)
		}
	}

	send := func() (*http.Response, error) {
		tok, err := g.bearerToken(ctx)
		if err != nil {
			return nil, err
		}
		var rdr io.Reader
		if raw != nil {
			rdr = bytes.NewReader(raw)
		}
		req, err := http.NewRequestWithContext(ctx, method, endpoint, rdr)
		if err != nil {
			return nil, fmt.Errorf("teams: build %s %s: %w", method, endpoint, err)
		}
		if raw != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Authorization", "Bearer "+tok)
		return g.httpc.Do(req)
	}

	resp, err := send()
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		// Token was rejected — drop the cache and retry exactly once.
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		g.invalidateToken()
		resp, err = send()
		if err != nil {
			return err
		}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("teams: read %s %s: %w", method, endpoint, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("teams: graph %s %s -> %d: %s", method, endpoint, resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	if dest == nil || len(bytes.TrimSpace(respBody)) == 0 {
		return nil
	}
	if err := json.Unmarshal(respBody, dest); err != nil {
		return fmt.Errorf("teams: decode %s %s: %w", method, endpoint, err)
	}
	return nil
}
