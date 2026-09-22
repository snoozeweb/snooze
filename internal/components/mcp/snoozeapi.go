package mcp

import (
	"context"

	"github.com/snoozeweb/snooze/pkg/snoozeclient"
)

// snoozeAPI is the narrow slice of the Snooze REST surface the MCP tools
// need. It exists so the Server can be unit-tested against a fake without a
// live snooze-server. *snoozeclient.Client satisfies it directly — Post and
// Get are its native methods, and the action helpers (PostComment /
// CreateSnooze) are the same wrappers the snooze-teams bridge uses.
//
// Endpoint mapping (mirrors internal/components/googlechat/forward.go and
// the snooze-teams handler):
//
//   - list_alerts   → POST /api/v1/record/search   {"condition": <Cond>}
//   - get_alert     → POST /api/v1/record/search   {"condition": ["=","uid",<uid>]}
//   - ack/close     → PostComment{Type:"ack"|"close", Method:"mcp"}
//   - comment       → PostComment{Type:"",          Method:"mcp"}
//   - snooze        → CreateSnooze{...}
//   - get/set agentic → GET/PUT /api/v1/record/{uid}/agentic
type snoozeAPI interface {
	// Post sends a JSON body to path and decodes the response into dest
	// (nil to skip). Used for the record/search lookups.
	Post(ctx context.Context, path string, body, dest any) error

	// PostComment posts a typed comment to /api/v1/comment. The server's
	// AfterCreate hook applies the ack/close state transition.
	PostComment(ctx context.Context, c snoozeclient.Comment) error

	// CreateSnooze posts a snooze entry to /api/v1/snooze.
	CreateSnooze(ctx context.Context, s snoozeclient.Snooze) error

	// Get fetches path and decodes the response into dest. Used to read an
	// alert's agentic analysis.
	Get(ctx context.Context, path string, dest any) error

	// Put replaces the resource at path. Used to store an agentic analysis,
	// the one write path the protected field accepts.
	Put(ctx context.Context, path string, body, dest any) error
}

// Compile-time proof the real client satisfies the interface.
var _ snoozeAPI = (*snoozeclient.Client)(nil)
