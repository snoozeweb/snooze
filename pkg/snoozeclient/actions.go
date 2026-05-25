package snoozeclient

// actions.go bundles the small helpers the snooze-teams bridge needs to
// drive ack / close / open / esc / snooze / comment from Teams chat
// commands. They sit alongside the existing alert helpers in alerts.go.
//
// These wrappers exist because:
//   - The Snooze REST surface is verb-rich (POST /comment with type=ack,
//     POST /snooze with a time_constraints window, etc.) and we want
//     compile-time-checked payload shapes rather than hand-rolling
//     map[string]any at every call site.
//   - The CRUD list endpoint encodes its condition filter as a
//     base64url(JSON)-encoded query parameter, which needs a dedicated
//     helper so callers don't have to know that wire detail.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"

	"github.com/snoozeweb/snooze/internal/condition"
)

// recordPath / commentPath / snoozePath: the Snooze v1 endpoints the action
// helpers target. Constants kept inline rather than in a shared file because
// they only have one call site each.
const (
	recordPath  = "/api/v1/record"
	commentPath = "/api/v1/comment"
	snoozePath  = "/api/v1/snooze"
)

// Comment is the wire shape POST /api/v1/comment accepts. type ∈
// {"", "ack", "close", "open", "esc"}; the empty string is a plain comment
// that doesn't change the record's state. Modifications is honored when
// type == "esc".
type Comment struct {
	RecordUID     string         `json:"record_uid"`
	Name          string         `json:"name,omitempty"`
	Method        string         `json:"method,omitempty"`
	Message       string         `json:"message,omitempty"`
	Type          string         `json:"type,omitempty"`
	Modifications [][]any        `json:"modifications,omitempty"`
	Extra         map[string]any `json:"-"`
}

// PostComment posts c to /api/v1/comment. The endpoint's AfterCreate hook
// applies the state transition for type ∈ {"ack","close","open","esc"} (see
// internal/pluginimpl/comment/plugin.go), so callers don't need to also
// PATCH the record.
func (c *Client) PostComment(ctx context.Context, com Comment) error {
	return c.Post(ctx, commentPath, com, nil)
}

// Snooze is the wire shape POST /api/v1/snooze accepts. The TimeConstraints
// field uses the same shape the notification plugin consumes; pass nil for
// a "forever" snooze.
type Snooze struct {
	Name            string         `json:"name"`
	Condition       any            `json:"condition,omitempty"` // condition.Cond AST or [op, field, value] list
	Comment         string         `json:"comment,omitempty"`
	TimeConstraints map[string]any `json:"time_constraints,omitempty"`
}

// CreateSnooze posts a single snooze entry. The Snooze server records it
// under its `snooze` collection; the notification plugin honors the
// time_constraints window on subsequent record matches.
func (c *Client) CreateSnooze(ctx context.Context, s Snooze) error {
	return c.Post(ctx, snoozePath, s, nil)
}

// SearchOpts controls SearchRecords pagination + ordering. All fields are
// optional; the zero value asks the server for its defaults.
type SearchOpts struct {
	PerPage int
	PageNb  int
	OrderBy string
	Asc     bool
}

// recordListResponse mirrors the {"data": [...], "meta": {...}} envelope
// the CRUD list endpoint returns. The Meta map is captured loosely because
// the v1 surface attaches different counters per collection.
type recordListResponse struct {
	Data []map[string]any `json:"data"`
	Meta map[string]any   `json:"meta,omitempty"`
}

// SearchRecords returns every record matching cond, paginated per opts.
// Records are returned as raw map[string]any so callers can read arbitrary
// fields the typed snoozetypes.Record schema doesn't model (e.g. the
// per-action `response_<name>` injection field that drives the Teams
// reply chain).
//
// cond is encoded as base64url(JSON) and passed via the `?q=` query
// parameter — the wire format the CRUD list handler expects.
func (c *Client) SearchRecords(ctx context.Context, cond condition.Cond, opts SearchOpts) ([]map[string]any, error) {
	q := url.Values{}
	if !cond.IsZero() {
		raw, err := json.Marshal(cond)
		if err != nil {
			return nil, fmt.Errorf("snoozeclient: marshal condition: %w", err)
		}
		q.Set("q", base64.RawURLEncoding.EncodeToString(raw))
	}
	if opts.PerPage > 0 {
		q.Set("perpage", strconv.Itoa(opts.PerPage))
	}
	if opts.PageNb > 0 {
		q.Set("pagenb", strconv.Itoa(opts.PageNb))
	}
	if opts.OrderBy != "" {
		q.Set("orderby", opts.OrderBy)
		q.Set("asc", strconv.FormatBool(opts.Asc))
	}
	path := recordPath
	if encoded := q.Encode(); encoded != "" {
		path += "?" + encoded
	}
	var resp recordListResponse
	if err := c.Get(ctx, path, &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}
