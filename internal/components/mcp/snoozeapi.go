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
type snoozeAPI interface {
	// Post sends a JSON body to path and decodes the response into dest
	// (nil to skip). Used for the record/search lookups.
	Post(ctx context.Context, path string, body, dest any) error

	// PostComment posts a typed comment to /api/v1/comment. The server's
	// AfterCreate hook applies the ack/close state transition.
	PostComment(ctx context.Context, c snoozeclient.Comment) error

	// CreateSnooze posts a snooze entry to /api/v1/snooze.
	CreateSnooze(ctx context.Context, s snoozeclient.Snooze) error
}

// Compile-time proof the real client satisfies the interface.
var _ snoozeAPI = (*snoozeclient.Client)(nil)
