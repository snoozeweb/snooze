package snoozeclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/cenkalti/backoff/v4"

	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// loginPathFor returns the v1 login endpoint for the given method.
func loginPathFor(method string) string {
	if method == "" {
		method = "local"
	}
	return "/api/v1/login/" + method
}

// loginRequest mirrors the wire shape accepted by /api/v1/login/{method}.
type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// loginResponse mirrors the wire shape returned on successful login.
type loginResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
	Method    string    `json:"method"`
}

// Client is the Snooze v1 REST client. It is safe for concurrent use.
type Client struct {
	opts   Options
	http   *http.Client
	logger *slog.Logger

	mu          sync.RWMutex
	token       string
	tokenLoaded bool // true once we've checked the cache file at least once
}

// APIError is the typed error returned for non-2xx responses whose body
// decoded into the canonical snoozetypes.ErrEnvelope.
type APIError struct {
	Status    int
	Code      string
	Message   string
	Details   map[string]any
	RequestID string
	TraceID   string
}

// Error implements the error interface. Status, code and message are
// concatenated so logs are useful at a glance.
func (e *APIError) Error() string {
	if e == nil {
		return "<nil api error>"
	}
	msg := e.Message
	if msg == "" {
		msg = http.StatusText(e.Status)
	}
	if e.Code != "" {
		return fmt.Sprintf("snooze: %d %s: %s", e.Status, e.Code, msg)
	}
	return fmt.Sprintf("snooze: %d: %s", e.Status, msg)
}

// IsAPIError extracts an *APIError from err, returning ok=false when err is
// not an APIError. Convenience for callers that want to branch on Status.
func IsAPIError(err error) (*APIError, bool) {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr, true
	}
	return nil, false
}

// New builds a Client from opts. It validates required fields, loads any
// cached token from disk, and returns a ready-to-use client. New does NOT
// perform a login; call Login explicitly (or rely on the lazy on-401 path).
func New(opts Options) (*Client, error) {
	resolved, cache, err := opts.resolve()
	if err != nil {
		// Non-fatal cache errors are swallowed; only ErrMissingBaseURL hard-fails.
		if errors.Is(err, ErrMissingBaseURL) {
			return nil, err
		}
		// The cache resolver may surface a fallback warning — log and continue.
		resolved.Logger.Warn("snoozeclient: cache fallback", slog.String("path", cache), slog.Any("err", err))
	}
	c := &Client{
		opts:   resolved,
		http:   resolved.buildHTTPClient(),
		logger: resolved.Logger,
	}
	// Seed token: explicit Options.Token wins; otherwise try cache.
	if resolved.Token != "" {
		c.token = resolved.Token
		c.tokenLoaded = true
	} else if cached, err := readTokenFile(resolved.TokenCacheFile); err == nil {
		c.token = cached
		c.tokenLoaded = true
	} else {
		c.logger.Debug("snoozeclient: ignoring cache error", slog.Any("err", err))
	}
	return c, nil
}

// BaseURL returns the normalised base URL.
func (c *Client) BaseURL() string { return c.opts.BaseURL }

// Token returns the current bearer token. Empty string when unauthenticated.
func (c *Client) Token() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.token
}

// SetToken overrides the bearer token in memory. It does not touch the on-disk
// cache; use Login to persist a fresh token.
func (c *Client) SetToken(tok string) {
	c.mu.Lock()
	c.token = tok
	c.tokenLoaded = true
	c.mu.Unlock()
}

// Logout clears the in-memory token and removes the on-disk cache file. It is
// idempotent and never errors on a missing cache file.
func (c *Client) Logout() error {
	c.mu.Lock()
	c.token = ""
	c.mu.Unlock()
	return deleteTokenFile(c.opts.TokenCacheFile)
}

// Login posts {username, password} to /api/v1/login/{method}, stores the
// returned token in memory and updates the on-disk cache.
//
// Network/5xx failures are retried per the configured backoff policy; 4xx
// is returned immediately. Login is safe to call concurrently — only one
// login runs at a time and the most-recent result wins.
func (c *Client) Login(ctx context.Context) error {
	if c.opts.Username == "" && c.opts.Method != "anonymous" {
		return errors.New("snoozeclient: Username is required for login")
	}
	body, err := json.Marshal(loginRequest{Username: c.opts.Username, Password: c.opts.Password}) //nolint:gosec
	if err != nil {
		return fmt.Errorf("snoozeclient: marshal login: %w", err)
	}
	url := c.opts.BaseURL + loginPathFor(c.opts.Method)
	var resp loginResponse
	op := func() error {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return backoff.Permanent(fmt.Errorf("snoozeclient: build login request: %w", err))
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		httpResp, err := c.http.Do(req)
		if err != nil {
			if isTransient(err) {
				return err
			}
			return backoff.Permanent(err)
		}
		defer httpResp.Body.Close() //nolint:errcheck
		if httpResp.StatusCode >= 200 && httpResp.StatusCode < 300 {
			if err := json.NewDecoder(httpResp.Body).Decode(&resp); err != nil {
				return backoff.Permanent(fmt.Errorf("snoozeclient: decode login response: %w", err))
			}
			return nil
		}
		apiErr := decodeAPIError(httpResp)
		if isRetriableStatus(httpResp.StatusCode) {
			return apiErr
		}
		return backoff.Permanent(apiErr)
	}
	if err := backoff.Retry(op, newBackoff(ctx, c.opts.InitialBackoff, c.opts.MaxRetries)); err != nil {
		return err
	}
	c.mu.Lock()
	c.token = resp.Token
	c.tokenLoaded = true
	c.mu.Unlock()
	if err := writeTokenFile(c.opts.TokenCacheFile, resp.Token); err != nil {
		c.logger.Warn("snoozeclient: failed to persist token cache", slog.Any("err", err))
	}
	return nil
}

// Get sends GET path and decodes the response into dest (nil to skip).
func (c *Client) Get(ctx context.Context, path string, dest any) error {
	return c.Do(ctx, http.MethodGet, path, nil, dest)
}

// Post sends POST path with body and decodes the response into dest (nil to skip).
func (c *Client) Post(ctx context.Context, path string, body, dest any) error {
	return c.Do(ctx, http.MethodPost, path, body, dest)
}

// Put sends PUT path with body and decodes the response into dest (nil to skip).
func (c *Client) Put(ctx context.Context, path string, body, dest any) error {
	return c.Do(ctx, http.MethodPut, path, body, dest)
}

// Patch sends PATCH path with body and decodes the response into dest (nil to skip).
func (c *Client) Patch(ctx context.Context, path string, body, dest any) error {
	return c.Do(ctx, http.MethodPatch, path, body, dest)
}

// Delete sends DELETE path and decodes the response into dest (nil to skip).
func (c *Client) Delete(ctx context.Context, path string, dest any) error {
	return c.Do(ctx, http.MethodDelete, path, nil, dest)
}

// Do is the low-level JSON helper used by Get/Post/Put/Patch/Delete. It:
//  1. Marshals body when non-nil.
//  2. Adds Authorization: Bearer <token> when a token is set.
//  3. Retries transient transport errors and retriable status codes.
//  4. On 401, attempts a one-shot re-Login and replays the request once.
//  5. On non-2xx, decodes the error envelope into *APIError.
//  6. On 2xx, decodes the body into dest (if non-nil and body is non-empty).
func (c *Client) Do(ctx context.Context, method, path string, body, dest any) error {
	var raw []byte
	if body != nil {
		switch b := body.(type) {
		case []byte:
			raw = b
		case json.RawMessage:
			raw = b
		default:
			var err error
			raw, err = json.Marshal(body)
			if err != nil {
				return fmt.Errorf("snoozeclient: marshal body: %w", err)
			}
		}
	}
	url := c.buildURL(path)
	// On a 401 we attempt exactly one re-login + replay. The `retriedAuth`
	// flag enforces the "once" guarantee across retry attempts.
	retriedAuth := false
	op := func() error {
		req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(raw))
		if err != nil {
			return backoff.Permanent(fmt.Errorf("snoozeclient: build request: %w", err))
		}
		if raw != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		req.Header.Set("Accept", "application/json")
		if c.opts.IngestToken != "" {
			req.Header.Set("Authorization", "Bearer "+c.opts.IngestToken)
		} else if tok := c.Token(); tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
		httpResp, err := c.http.Do(req)
		if err != nil {
			if isTransient(err) {
				return err
			}
			return backoff.Permanent(err)
		}
		defer httpResp.Body.Close() //nolint:errcheck

		if httpResp.StatusCode == http.StatusUnauthorized && !retriedAuth && c.canRelogin() {
			retriedAuth = true
			// Drain and discard the body so the connection can be reused.
			_, _ = io.Copy(io.Discard, httpResp.Body)
			if err := c.Login(ctx); err != nil {
				return backoff.Permanent(fmt.Errorf("snoozeclient: re-login after 401: %w", err))
			}
			// Fall through to the retriable branch so the next iteration replays.
			return errors.New("snoozeclient: retrying after 401")
		}
		if httpResp.StatusCode >= 200 && httpResp.StatusCode < 300 {
			if dest != nil {
				if err := decodeBody(httpResp, dest); err != nil {
					return backoff.Permanent(err)
				}
			}
			return nil
		}
		apiErr := decodeAPIError(httpResp)
		if isRetriableStatus(httpResp.StatusCode) {
			return apiErr
		}
		return backoff.Permanent(apiErr)
	}
	return backoff.Retry(op, newBackoff(ctx, c.opts.InitialBackoff, c.opts.MaxRetries))
}

// canRelogin reports whether the client has credentials to re-authenticate.
func (c *Client) canRelogin() bool {
	if c.opts.Method == "anonymous" {
		return true
	}
	return c.opts.Username != "" && c.opts.Password != ""
}

// buildURL appends path to BaseURL, accepting both leading-slash and
// already-qualified paths.
func (c *Client) buildURL(path string) string {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return c.opts.BaseURL + path
}

// decodeBody reads up to ~8 MiB and decodes into dest. Empty bodies are a no-op.
func decodeBody(resp *http.Response, dest any) error {
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("snoozeclient: read body: %w", err)
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, dest); err != nil {
		return fmt.Errorf("snoozeclient: decode body: %w", err)
	}
	return nil
}

// decodeAPIError turns a non-2xx *http.Response into an *APIError. When the
// body doesn't decode as a snoozetypes.ErrEnvelope the raw text is preserved
// in Message so callers still have something to log.
func decodeAPIError(resp *http.Response) *APIError {
	apiErr := &APIError{Status: resp.StatusCode}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		apiErr.Message = fmt.Sprintf("read error body: %v", err)
		return apiErr
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		apiErr.Message = http.StatusText(resp.StatusCode)
		return apiErr
	}
	var env snoozetypes.ErrEnvelope
	if err := json.Unmarshal(raw, &env); err == nil && env.Error.Code != "" {
		apiErr.Code = env.Error.Code
		apiErr.Message = env.Error.Message
		apiErr.Details = env.Error.Details
		apiErr.RequestID = env.Error.RequestID
		apiErr.TraceID = env.Error.TraceID
		return apiErr
	}
	// Fall back to raw body as the message.
	apiErr.Message = strings.TrimSpace(string(raw))
	return apiErr
}
