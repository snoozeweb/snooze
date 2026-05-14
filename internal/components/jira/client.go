package jira

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// retryAttempts mirrors the Python plugin's retry budget (3 tries total).
const retryAttempts = 3

// retryBackoff is the fixed sleep between retries. The JIRA Cloud rate
// limits are gentle and this is plenty; we don't bother with exponential
// backoff here because the inbound webhook is already a synchronous request
// from snooze-server and we want predictable upper bounds.
const retryBackoff = time.Second

// Client wraps the subset of the JIRA Cloud REST v3 API the daemon exercises.
// It is safe for concurrent use.
type Client struct {
	baseURL string
	auth    string
	hc      *http.Client
	logger  *slog.Logger
}

// ClientOptions bundles the knobs NewClient understands. Email + Token are
// the basic-auth credentials (an Atlassian Cloud API token, NOT a password).
type ClientOptions struct {
	BaseURL    string
	Email      string
	Token      string
	VerifySSL  bool
	Timeout    time.Duration
	HTTPClient *http.Client
	Logger     *slog.Logger
}

// NewClient builds a Client. The basic-auth header is computed once and
// cached on the struct; callers must rotate credentials by building a fresh
// client.
func NewClient(opts ClientOptions) *Client {
	if opts.Timeout <= 0 {
		opts.Timeout = 30 * time.Second
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	hc := opts.HTTPClient
	if hc == nil {
		tr := http.DefaultTransport.(*http.Transport).Clone()
		if !opts.VerifySSL {
			tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // opt-in
		}
		hc = &http.Client{Timeout: opts.Timeout, Transport: tr}
	}
	auth := base64.StdEncoding.EncodeToString([]byte(opts.Email + ":" + opts.Token))
	return &Client{
		baseURL: strings.TrimRight(opts.BaseURL, "/"),
		auth:    "Basic " + auth,
		hc:      hc,
		logger:  opts.Logger,
	}
}

// apiError captures the JIRA error envelope ({"errorMessages":[…],"errors":{}}).
// We surface it via *Error so callers can branch on the message text (e.g.
// the priority-as-string fallback).
type Error struct {
	Status        int
	Method        string
	Path          string
	Body          string
	ErrorMessages []string
	FieldErrors   map[string]string
}

// Error implements error.
func (e *Error) Error() string {
	if e == nil {
		return "<nil jira error>"
	}
	if len(e.ErrorMessages) > 0 || len(e.FieldErrors) > 0 {
		return fmt.Sprintf("jira: %s %s: %d: errorMessages=%v fieldErrors=%v",
			e.Method, e.Path, e.Status, e.ErrorMessages, e.FieldErrors)
	}
	return fmt.Sprintf("jira: %s %s: %d: %s", e.Method, e.Path, e.Status, strings.TrimSpace(e.Body))
}

// errorEnvelope is the wire shape of a JIRA error body.
type errorEnvelope struct {
	ErrorMessages []string          `json:"errorMessages"`
	Errors        map[string]string `json:"errors"`
}

// IsError extracts a *Error from err.
func IsError(err error) (*Error, bool) {
	var je *Error
	if errors.As(err, &je) {
		return je, true
	}
	return nil, false
}

// do issues an authenticated request and decodes the JSON body into dest
// (when non-nil). Non-2xx responses are surfaced as *Error after retries.
func (c *Client) do(ctx context.Context, method, path string, body, dest any) error {
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
				return fmt.Errorf("jira: marshal body: %w", err)
			}
		}
	}
	fullURL := c.baseURL + "/rest/api/3" + path
	var lastErr error
	for attempt := 0; attempt < retryAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, method, fullURL, bytes.NewReader(raw))
		if err != nil {
			return fmt.Errorf("jira: build request: %w", err)
		}
		req.Header.Set("Authorization", c.auth)
		req.Header.Set("Accept", "application/json")
		if raw != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := c.hc.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("jira: %s %s: %w", method, path, err)
			c.logger.Warn("jira: request failed", slog.Int("attempt", attempt+1), slog.Any("err", err))
			if attempt < retryAttempts-1 {
				if !sleepCtx(ctx, retryBackoff) {
					return ctx.Err()
				}
				continue
			}
			return lastErr
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			err := decodeResponse(resp, dest)
			resp.Body.Close()
			return err
		}
		jerr := decodeError(resp, method, path)
		resp.Body.Close()
		lastErr = jerr
		// 4xx (except 429) are not retried — they are operator errors that
		// retrying won't fix. 5xx and 429 get retried up to retryAttempts.
		retriable := jerr.Status == http.StatusTooManyRequests || jerr.Status >= 500
		c.logger.Warn("jira: api error",
			slog.Int("status", jerr.Status),
			slog.String("method", method),
			slog.String("path", path),
			slog.Any("err", jerr))
		if !retriable || attempt == retryAttempts-1 {
			return lastErr
		}
		if !sleepCtx(ctx, retryBackoff) {
			return ctx.Err()
		}
	}
	return lastErr
}

// decodeResponse reads up to 8 MiB of body and decodes it into dest when
// non-nil. An empty body is a no-op even if dest is set so 204-style replies
// don't trip the decoder.
func decodeResponse(resp *http.Response, dest any) error {
	if dest == nil || resp.StatusCode == http.StatusNoContent {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("jira: read body: %w", err)
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, dest); err != nil {
		return fmt.Errorf("jira: decode body: %w", err)
	}
	return nil
}

// decodeError turns a non-2xx *http.Response into a typed *Error. When the
// body decodes as the canonical envelope we surface the fields; otherwise
// the raw body is preserved so logs aren't useless.
func decodeError(resp *http.Response, method, path string) *Error {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	jerr := &Error{
		Status: resp.StatusCode,
		Method: method,
		Path:   path,
		Body:   string(raw),
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return jerr
	}
	var env errorEnvelope
	if err := json.Unmarshal(raw, &env); err == nil {
		jerr.ErrorMessages = env.ErrorMessages
		jerr.FieldErrors = env.Errors
	}
	return jerr
}

// sleepCtx pauses for d unless ctx is cancelled first. Returns false when
// ctx was cancelled.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// ----------------------------------------------------------------------------
// Typed helpers around /rest/api/3/*
// ----------------------------------------------------------------------------

// CreateIssueRequest is the structured payload sent to POST /issue.
type CreateIssueRequest struct {
	ProjectKey   string
	IssueType    string
	IssueTypeID  string
	Summary      string
	Description  ADF
	Priority     string
	Labels       []string
	ExtraFields  map[string]any
}

// CreateIssueResponse mirrors the relevant fields of the POST /issue reply.
type CreateIssueResponse struct {
	ID   string `json:"id"`
	Key  string `json:"key"`
	Self string `json:"self"`
}

// CreateIssue posts a new issue. On the first try priority is sent as a
// `{name: …}` object; some on-prem JIRA configurations require a bare string,
// so we retry once with the alternate format when that specific 400 surfaces.
func (c *Client) CreateIssue(ctx context.Context, req CreateIssueRequest) (CreateIssueResponse, error) {
	fields := map[string]any{
		"project":     map[string]any{"key": req.ProjectKey},
		"summary":     req.Summary,
		"description": req.Description,
	}
	if req.IssueTypeID != "" {
		fields["issuetype"] = map[string]any{"id": req.IssueTypeID}
	} else if req.IssueType != "" {
		fields["issuetype"] = map[string]any{"name": req.IssueType}
	}
	if req.Priority != "" {
		fields["priority"] = map[string]any{"name": req.Priority}
	}
	if len(req.Labels) > 0 {
		fields["labels"] = req.Labels
	}
	for k, v := range req.ExtraFields {
		fields[k] = v
	}
	body := map[string]any{"fields": fields}

	var resp CreateIssueResponse
	err := c.do(ctx, http.MethodPost, "/issue", body, &resp)
	if err != nil && req.Priority != "" && priorityNeedsString(err) {
		// Retry with priority as a bare string. We rebuild fields so the
		// original payload is untouched in case the caller logs both.
		fallback := map[string]any{}
		for k, v := range fields {
			fallback[k] = v
		}
		fallback["priority"] = req.Priority
		body = map[string]any{"fields": fallback}
		c.logger.Info("jira: retrying CreateIssue with string priority")
		err = c.do(ctx, http.MethodPost, "/issue", body, &resp)
	}
	return resp, err
}

// priorityNeedsString reports whether err is the specific JIRA 400 telling us
// to send `priority` as a string rather than an object.
func priorityNeedsString(err error) bool {
	je, ok := IsError(err)
	if !ok {
		return false
	}
	if je.Status != http.StatusBadRequest {
		return false
	}
	p := strings.ToLower(je.FieldErrors["priority"])
	return p != "" && (strings.Contains(p, "string") || strings.Contains(p, "cha"))
}

// AddComment posts a plain-text comment to issueKey. The text is wrapped in
// ADF.
func (c *Client) AddComment(ctx context.Context, issueKey, text string) error {
	body := map[string]any{"body": textADF(text)}
	return c.do(ctx, http.MethodPost, "/issue/"+url.PathEscape(issueKey)+"/comment", body, nil)
}

// Transition applies a transition (by id) to issueKey and optionally
// includes a comment in the same payload.
func (c *Client) Transition(ctx context.Context, issueKey, transitionID, comment string) error {
	body := map[string]any{"transition": map[string]any{"id": transitionID}}
	if comment != "" {
		body["update"] = map[string]any{
			"comment": []map[string]any{{
				"add": map[string]any{"body": textADF(comment)},
			}},
		}
	}
	return c.do(ctx, http.MethodPost, "/issue/"+url.PathEscape(issueKey)+"/transitions", body, nil)
}

// IssueStatus is the trimmed status block of an issue, sufficient to detect
// "done" tickets.
type IssueStatus struct {
	Name     string `json:"name"`
	Category struct {
		Key  string `json:"key"`
		Name string `json:"name"`
	} `json:"statusCategory"`
}

// IssueFields carries only the bits the daemon actually reads back from
// /issue/{key}. New fields can be added without breaking compatibility.
type IssueFields struct {
	Status IssueStatus `json:"status"`
}

// Issue is the minimal /issue/{key} response.
type Issue struct {
	ID     string      `json:"id"`
	Key    string      `json:"key"`
	Fields IssueFields `json:"fields"`
}

// GetIssue fetches /issue/{key}.
func (c *Client) GetIssue(ctx context.Context, key string) (Issue, error) {
	var out Issue
	err := c.do(ctx, http.MethodGet, "/issue/"+url.PathEscape(key), nil, &out)
	return out, err
}

// TransitionEntry is one element of GET /issue/{key}/transitions.
type TransitionEntry struct {
	ID   string      `json:"id"`
	Name string      `json:"name"`
	To   IssueStatus `json:"to"`
}

// transitionsResponse wraps the GET /transitions reply.
type transitionsResponse struct {
	Transitions []TransitionEntry `json:"transitions"`
}

// GetTransitions returns the available transitions for issueKey.
func (c *Client) GetTransitions(ctx context.Context, issueKey string) ([]TransitionEntry, error) {
	var out transitionsResponse
	err := c.do(ctx, http.MethodGet, "/issue/"+url.PathEscape(issueKey)+"/transitions", nil, &out)
	return out.Transitions, err
}

// SearchIssue is the trimmed shape we read from /search/jql. The fields map
// retains the raw JSON for arbitrary custom-field types — callers extract
// what they need.
type SearchIssue struct {
	ID     string         `json:"id"`
	Key    string         `json:"key"`
	Fields map[string]any `json:"fields"`
}

// searchResponse wraps POST /search/jql.
type searchResponse struct {
	Issues []SearchIssue `json:"issues"`
	Total  int           `json:"total"`
}

// Search issues a JQL query. fields enumerates the field IDs to include in
// each issue (passing nil returns the default navigable set, which is
// expensive — always specify fields explicitly).
func (c *Client) Search(ctx context.Context, jql string, fields []string, maxResults int) ([]SearchIssue, error) {
	if maxResults <= 0 {
		maxResults = 50
	}
	body := map[string]any{
		"jql":        jql,
		"maxResults": maxResults,
	}
	if len(fields) > 0 {
		body["fields"] = fields
	}
	var resp searchResponse
	if err := c.do(ctx, http.MethodPost, "/search/jql", body, &resp); err != nil {
		return nil, err
	}
	return resp.Issues, nil
}

// userSearchResult is the trimmed /user/search response.
type userSearchResult struct {
	AccountID    string `json:"accountId"`
	EmailAddress string `json:"emailAddress"`
}

// FindUserByEmail resolves email to a JIRA accountId via /user/search.
// Returns an empty string when no user matches.
func (c *Client) FindUserByEmail(ctx context.Context, email string) (string, error) {
	q := url.Values{}
	q.Set("query", email)
	path := "/user/search?" + q.Encode()
	var users []userSearchResult
	if err := c.do(ctx, http.MethodGet, path, nil, &users); err != nil {
		return "", err
	}
	if len(users) == 0 {
		return "", nil
	}
	if len(users) == 1 {
		return users[0].AccountID, nil
	}
	// Multiple matches: prefer an exact-case-insensitive match on emailAddress.
	for _, u := range users {
		if strings.EqualFold(u.EmailAddress, email) {
			return u.AccountID, nil
		}
	}
	return users[0].AccountID, nil
}
