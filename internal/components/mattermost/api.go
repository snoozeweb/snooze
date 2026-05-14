package mattermost

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// mmAPI is a tiny Mattermost v4 REST helper. We avoid the upstream Go SDK
// (github.com/mattermost/mattermost/server/public/model) because it pulls
// in the full server module and dozens of unrelated dependencies. Only the
// handful of endpoints exercised by the daemon are wrapped here.
//
//	GET  /api/v4/users/me                       — sanity check the token
//	GET  /api/v4/teams/name/{team}              — resolve team name → ID
//	GET  /api/v4/teams/{teamID}/channels/name/{ch} — resolve channel name → ID
//	POST /api/v4/posts                          — create a message/reply
type mmAPI struct {
	baseURL string
	token   string
	hc      *http.Client
}

// User is the (slimmed) /users/me response shape.
type User struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

// Team is the (slimmed) /teams response shape.
type Team struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
}

// Channel is the (slimmed) /channels response shape.
type Channel struct {
	ID          string `json:"id"`
	TeamID      string `json:"team_id"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
}

// Post is the request/response shape for /api/v4/posts.
type Post struct {
	ID        string `json:"id,omitempty"`
	ChannelID string `json:"channel_id"`
	Message   string `json:"message"`
	RootID    string `json:"root_id,omitempty"`
}

// newAPI builds an mmAPI bound to baseURL with the given personal access token.
// insecure=true disables TLS verification (opt-in for self-signed dev setups).
func newAPI(baseURL, token string, insecure bool) *mmAPI {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if insecure {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // opt-in
	}
	return &mmAPI{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		hc:      &http.Client{Timeout: 20 * time.Second, Transport: tr},
	}
}

// do issues an authenticated JSON request and decodes the body into dest
// (when non-nil). Non-2xx responses are returned as plain errors carrying
// the status + body so the caller can log a useful message.
func (a *mmAPI) do(ctx context.Context, method, path string, body, dest any) error {
	var rdr io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("mattermost api: marshal body: %w", err)
		}
		rdr = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, a.baseURL+path, rdr)
	if err != nil {
		return fmt.Errorf("mattermost api: build request: %w", err)
	}
	if a.token != "" {
		req.Header.Set("Authorization", "Bearer "+a.token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	resp, err := a.hc.Do(req)
	if err != nil {
		return fmt.Errorf("mattermost api: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return fmt.Errorf("mattermost api: %s %s: %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if dest != nil {
		if err := json.NewDecoder(resp.Body).Decode(dest); err != nil {
			return fmt.Errorf("mattermost api: decode %s: %w", path, err)
		}
	}
	return nil
}

// Me probes /users/me, returning the bot's own user info. Useful as a smoke
// test that the personal access token is valid before opening the WebSocket.
func (a *mmAPI) Me(ctx context.Context) (*User, error) {
	var u User
	if err := a.do(ctx, http.MethodGet, "/api/v4/users/me", nil, &u); err != nil {
		return nil, err
	}
	return &u, nil
}

// TeamByName resolves a team slug to its server-side ID.
func (a *mmAPI) TeamByName(ctx context.Context, name string) (*Team, error) {
	if name == "" {
		return nil, errors.New("mattermost api: team name is required")
	}
	var t Team
	if err := a.do(ctx, http.MethodGet, "/api/v4/teams/name/"+url.PathEscape(name), nil, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

// ChannelByName resolves a (team, channel-name) pair to its server-side ID.
func (a *mmAPI) ChannelByName(ctx context.Context, teamID, name string) (*Channel, error) {
	if teamID == "" || name == "" {
		return nil, errors.New("mattermost api: teamID and channel name are required")
	}
	path := "/api/v4/teams/" + url.PathEscape(teamID) + "/channels/name/" + url.PathEscape(name)
	var ch Channel
	if err := a.do(ctx, http.MethodGet, path, nil, &ch); err != nil {
		return nil, err
	}
	return &ch, nil
}

// CreatePost posts a message to a channel (or replies in a thread when RootID
// is non-empty) and returns the server-side Post (with ID populated).
func (a *mmAPI) CreatePost(ctx context.Context, p Post) (*Post, error) {
	if p.ChannelID == "" {
		return nil, errors.New("mattermost api: ChannelID is required")
	}
	var out Post
	if err := a.do(ctx, http.MethodPost, "/api/v4/posts", p, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
