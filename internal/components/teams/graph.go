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

// authMode discriminates the OAuth2 flow the daemon uses to talk to Graph.
//
//   - authModeApp: client_credentials with a confidential client secret. Used
//     historically and still appropriate for read-only scopes the tenant has
//     granted as application permissions.
//   - authModeDelegated: refresh_token rotation against a refresh token
//     obtained one-shot via the device-code flow (see authorize.go). Required
//     for ChannelMessage.Send, which Microsoft does not expose as an
//     application permission.
type authMode int

const (
	authModeApp authMode = iota
	authModeDelegated
)

// graphClient is a tiny Microsoft Graph wrapper. It handles:
//
//   - OAuth2 token acquisition under either authModeApp (client_credentials)
//     or authModeDelegated (refresh_token), with caching + auto-refresh.
//   - POST /teams/{team}/channels/{channel}/messages.
//   - GET  /teams/{team}/channels/{channel}/messages (paged "value" envelope).
//
// It is intentionally narrow: no msgraph-sdk-go dependency, only stdlib.
type graphClient struct {
	httpc *http.Client

	mode authMode

	tenantID     string
	clientID     string
	clientSecret string // app mode only
	// publicClient is true when the AAD app registration is configured for
	// the "Mobile and desktop applications" platform. AAD then rejects any
	// token request that carries a client_secret with AADSTS700025; we
	// suppress the field for delegated-mode refresh calls in that case.
	publicClient bool
	scope        string

	store *tokenStore // delegated mode only

	loginBase string
	graphBase string

	mu        sync.Mutex
	token     string
	expiresAt time.Time
}

// newGraphClient builds a graph client from Config. It does not perform any
// network I/O; the first FetchToken / API call triggers the OAuth2 round trip.
// When cfg.AuthMode is "delegated", the returned client reads its refresh
// token from cfg.TokenFile via a tokenStore — that file must already have
// been populated by `snooze-teams authorize`.
func newGraphClient(cfg Config, httpc *http.Client) *graphClient {
	if httpc == nil {
		httpc = &http.Client{Timeout: cfg.RequestTimeout}
	}
	mode := authModeApp
	var store *tokenStore
	scope := cfg.Scope
	if strings.EqualFold(cfg.AuthMode, "delegated") {
		mode = authModeDelegated
		store = newTokenStore(cfg.TokenFile)
		// For delegated refresh_token grants we re-request the delegated
		// scope list, not the .default app scope. Joining with spaces
		// matches the AAD v2 token endpoint's wire format.
		scope = strings.Join(cfg.Scopes, " ")
	}
	return &graphClient{
		httpc:        httpc,
		mode:         mode,
		tenantID:     cfg.TenantID,
		clientID:     cfg.ClientID,
		clientSecret: cfg.ClientSecret,
		publicClient: cfg.PublicClient,
		scope:        scope,
		store:        store,
		loginBase:    cfg.LoginBase,
		graphBase:    cfg.GraphBase,
	}
}

// graphTokenResponse mirrors the wire shape of the v2 token endpoint. The
// refresh_token field is empty for client_credentials responses and populated
// (subject to AAD rotation policy) for refresh_token responses.
type graphTokenResponse struct {
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

// graphTokenError mirrors the AAD error envelope: an opaque code plus a
// long human-readable description.
type graphTokenError struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

// tokenErr is the structured error postTokenForm returns when AAD rejects a
// token request. The `code` field is the AAD `error` token (invalid_client,
// invalid_grant, …) and `desc` is the long description that usually carries
// an `AADSTSnnnnn:` prefix. refreshHint walks the description for codes we
// know how to translate into actionable advice.
type tokenErr struct {
	status int
	code   string
	desc   string
}

func (e *tokenErr) Error() string {
	return fmt.Sprintf("teams: token %d %s: %s", e.status, e.code, e.desc)
}

// refreshHint wraps a postTokenForm error with a one-line, operator-actionable
// hint when the AAD failure code is one we recognise. Otherwise it returns
// err unchanged. The hint replaces the prior blanket "run `snooze-teams
// authorize` if revoked" suggestion, which was misleading for the common
// AADSTS700025 (public-client / client_secret mismatch) and AADSTS700082
// (refresh token expired) cases.
func refreshHint(err error) error {
	var te *tokenErr
	if !errors.As(err, &te) {
		return err
	}
	switch {
	case strings.Contains(te.desc, "AADSTS700025"):
		// "Client is public so neither client_assertion nor client_secret should be presented"
		return fmt.Errorf("%w (hint: the AAD app is a public client — set public_client: true in teams.yaml, or remove client_secret)", err)
	case strings.Contains(te.desc, "AADSTS700082"), strings.Contains(te.desc, "AADSTS700081"):
		// Refresh token expired or revoked.
		return fmt.Errorf("%w (hint: refresh token expired — re-run `snooze-teams authorize`)", err)
	case strings.Contains(te.desc, "AADSTS7000215"), strings.Contains(te.desc, "AADSTS7000222"):
		// Wrong / expired client_secret.
		return fmt.Errorf("%w (hint: client_secret is wrong or expired — rotate it in Azure and update teams.yaml)", err)
	case te.code == "invalid_grant":
		return fmt.Errorf("%w (hint: the cached refresh token is no longer accepted — re-run `snooze-teams authorize`)", err)
	default:
		return err
	}
}

// fetchToken acquires a fresh access token from AAD, choosing the grant type
// based on the client's configured auth mode. The result is cached on the
// client and, for delegated mode, persisted to the token store so the next
// daemon restart can reuse it without burning a refresh round-trip.
func (g *graphClient) fetchToken(ctx context.Context) (string, error) {
	if g.mode == authModeDelegated {
		return g.fetchDelegatedToken(ctx)
	}
	return g.fetchAppToken(ctx)
}

// fetchAppToken runs the client_credentials grant against AAD. This is the
// classic 1.x-era flow and remains valid for read-only application scopes.
func (g *graphClient) fetchAppToken(ctx context.Context) (string, error) {
	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", g.clientID)
	form.Set("client_secret", g.clientSecret)
	form.Set("scope", g.scope)

	tr, err := g.postTokenForm(ctx, form)
	if err != nil {
		return "", err
	}
	g.cacheAccessToken(tr.AccessToken, tr.ExpiresIn)
	return tr.AccessToken, nil
}

// fetchDelegatedToken refreshes the cached delegated grant. AAD may rotate
// the refresh_token on every call (the default for confidential clients with
// refresh-token rotation enabled), so a new token gets written back to the
// store as soon as it is acquired.
func (g *graphClient) fetchDelegatedToken(ctx context.Context) (string, error) {
	if g.store == nil {
		return "", errors.New("teams: delegated mode requires a token file path")
	}
	cached, err := g.store.Load()
	if err != nil {
		return "", err
	}
	// If the cached access token is still valid, reuse it — this keeps the
	// daemon warm across short restarts without an extra refresh round-trip.
	if cached.AccessToken != "" && time.Now().Before(cached.ExpiresAt.Add(-graphTokenSkew)) {
		g.cacheAccessTokenAt(cached.AccessToken, cached.ExpiresAt)
		return cached.AccessToken, nil
	}

	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", cached.RefreshToken)
	form.Set("client_id", g.clientID)
	// Public-client AAD apps reject token requests that include a
	// client_secret (AADSTS700025). The operator declares the app's
	// platform via the public_client flag; we honour it strictly so a
	// stale or copy-pasted client_secret in teams.yaml stops being
	// silently appended to every refresh.
	if !g.publicClient && g.clientSecret != "" {
		form.Set("client_secret", g.clientSecret)
	}
	if g.scope != "" {
		form.Set("scope", g.scope)
	}

	tr, err := g.postTokenForm(ctx, form)
	if err != nil {
		return "", fmt.Errorf("teams: refresh token grant failed: %w", refreshHint(err))
	}

	expiresAt := time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)
	newRefresh := tr.RefreshToken
	if newRefresh == "" {
		// AAD did not rotate the refresh token — keep the previous one
		// rather than wiping it.
		newRefresh = cached.RefreshToken
	}
	saved := cachedToken{
		AccessToken:  tr.AccessToken,
		RefreshToken: newRefresh,
		ExpiresAt:    expiresAt,
		Scope:        tr.Scope,
		ObtainedAt:   time.Now(),
		TenantID:     cached.TenantID,
		ClientID:     cached.ClientID,
	}
	if err := g.store.Save(saved); err != nil {
		// A failure to persist is not fatal — we still have the in-memory
		// access token. Surface it so operators notice the cache is wedged.
		return tr.AccessToken, fmt.Errorf("teams: token persisted in memory only (disk save failed: %w)", err)
	}
	g.cacheAccessTokenAt(tr.AccessToken, expiresAt)
	return tr.AccessToken, nil
}

// postTokenForm submits form to the v2.0 token endpoint and returns the
// parsed body, or a structured error containing AAD's `error` /
// `error_description` pair when AAD rejects the request.
func (g *graphClient) postTokenForm(ctx context.Context, form url.Values) (graphTokenResponse, error) {
	endpoint := fmt.Sprintf("%s/%s/oauth2/v2.0/token", g.loginBase, g.tenantID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return graphTokenResponse{}, fmt.Errorf("teams: build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := g.httpc.Do(req)
	if err != nil {
		return graphTokenResponse{}, fmt.Errorf("teams: token request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return graphTokenResponse{}, fmt.Errorf("teams: read token response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var apiErr graphTokenError
		if jerr := json.Unmarshal(raw, &apiErr); jerr == nil && apiErr.Error != "" {
			return graphTokenResponse{}, &tokenErr{
				status: resp.StatusCode,
				code:   apiErr.Error,
				desc:   apiErr.ErrorDescription,
			}
		}
		return graphTokenResponse{}, fmt.Errorf("teams: token %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var tr graphTokenResponse
	if err := json.Unmarshal(raw, &tr); err != nil {
		return graphTokenResponse{}, fmt.Errorf("teams: decode token response: %w", err)
	}
	if tr.AccessToken == "" {
		return graphTokenResponse{}, errors.New("teams: token response missing access_token")
	}
	return tr, nil
}

// cacheAccessToken stores tok in memory with a deadline derived from
// expiresIn (subject to graphTokenSkew). expiresIn ≤ 0 falls back to 1h.
func (g *graphClient) cacheAccessToken(tok string, expiresIn int) {
	ttl := time.Duration(expiresIn) * time.Second
	if ttl <= 0 {
		ttl = time.Hour
	}
	g.cacheAccessTokenAt(tok, time.Now().Add(ttl))
}

// cacheAccessTokenAt stores tok in memory with an explicit expiry instant.
func (g *graphClient) cacheAccessTokenAt(tok string, expiresAt time.Time) {
	g.mu.Lock()
	g.token = tok
	g.expiresAt = expiresAt.Add(-graphTokenSkew)
	g.mu.Unlock()
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
// safety margin (e.g. tenant admin rotated the secret or revoked the
// refresh-token family).
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
	ID              string          `json:"id"`
	ReplyToID       string          `json:"replyToId,omitempty"`
	CreatedDateTime time.Time       `json:"createdDateTime,omitempty"`
	From            graphFrom       `json:"from"`
	Body            graphBody       `json:"body"`
	Mentions        []graphMention  `json:"mentions,omitempty"`
	Raw             json.RawMessage `json:"-"`
	Extra           map[string]any  `json:"-"`
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
	ID          int            `json:"id"`
	MentionText string         `json:"mentionText"`
	Mentioned   map[string]any `json:"mentioned,omitempty"`
}

// graphMessagesPage is the {"value": [...]} envelope returned by GET messages.
type graphMessagesPage struct {
	Value []graphMessage `json:"value"`
}

// chatAttachment is a single entry in the chatMessage `attachments` array.
// For Adaptive Cards, contentType is "application/vnd.microsoft.card.adaptive"
// and content is the serialized card JSON (a string, not a nested object —
// that's a Graph quirk).
type chatAttachment struct {
	ID           string  `json:"id"`
	ContentType  string  `json:"contentType"`
	ContentURL   *string `json:"contentUrl"`
	Content      string  `json:"content"`
	Name         *string `json:"name"`
	ThumbnailURL *string `json:"thumbnailUrl"`
}

// sendOpts bundles the optional knobs of sendMessage. Kept as a struct so
// the call sites stay readable when several fields are set.
type sendOpts struct {
	// Attachments, when non-empty, are referenced from htmlBody via
	// `<attachment id="..."></attachment>` placeholders.
	Attachments []chatAttachment
	// ReplyToID, when non-empty, posts the message as a reply to the
	// channel message with that id. Targets the Graph
	// `/teams/{t}/channels/{c}/messages/{root}/replies` endpoint, which
	// behaves the same as the top-level endpoint except that the new
	// message lands in the thread under {root} instead of as a new thread.
	ReplyToID string
}

// sendMessage POSTs a chatMessage to the configured channel. htmlBody is the
// outer body content. When opts.ReplyToID is set, the message becomes a
// reply under that root and lands on the Graph `/replies` endpoint.
func (g *graphClient) sendMessage(ctx context.Context, teamID, channelID, htmlBody string, opts sendOpts) (graphMessage, error) {
	var endpoint string
	if opts.ReplyToID != "" {
		endpoint = fmt.Sprintf("%s/teams/%s/channels/%s/messages/%s/replies",
			g.graphBase, teamID, channelID, opts.ReplyToID)
	} else {
		endpoint = fmt.Sprintf("%s/teams/%s/channels/%s/messages",
			g.graphBase, teamID, channelID)
	}
	payload := map[string]any{
		"body": map[string]any{
			"contentType": "html",
			"content":     htmlBody,
		},
	}
	if len(opts.Attachments) > 0 {
		payload["attachments"] = opts.Attachments
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
	defer resp.Body.Close() //nolint:errcheck

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
