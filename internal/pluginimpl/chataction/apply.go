// Package chataction is the shared helper behind the Plan 36 chat-interactive
// webhook receivers (slackinteractive, telegraminteractive). It applies an
// ack/close/open state transition requested from a chat message button by
// writing an ATTRIBUTED comment through the SAME create sequence the generic
// CRUD layer uses — so the comment plugin's Plan-05 transition guard
// (GuardWrite) AND its state-application side effect (AfterCreate: sets
// record.state, bumps comment_count, stamps the lifecycle deadlines) both fire.
//
// There is no raw `record.state` poke here: the comment-create path is the
// single source of truth for a state transition, identical to what
// POST /api/v1/comment does. The helper never imports a concrete DB driver — it
// goes through host.DB() for the pre-check lookup and through the comment plugin
// (host.Plugin("comment"), cast to the plugins hook interfaces) for the write.
package chataction

import (
	"context"
	"errors"
	"fmt"

	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/pluginimpl/comment"
	"github.com/snoozeweb/snooze/internal/plugins"
)

// validActions is the set of chat-button verbs accepted from a message. Snooze
// drops Alerta's watch/unwatch/blackout (no tag-watch model), so the chat
// surface is the canonical lifecycle trio plus escalate.
var validActions = map[string]bool{
	"ack":   true,
	"close": true,
	"open":  true,
	"esc":   true,
}

// Apply validates and applies a chat-button state transition for recordUID and
// returns the record's new state.
//
// It resolves the record within the caller's context — the receivers run behind
// IngestTenant, so ctx carries the request's tenant and every lookup/write is
// tenant-scoped (never a global lookup). actor is the chat username (free-text
// channel attribution, NOT a Snooze auth subject); channel is the originating
// surface ("slack"/"telegram"), stamped into the comment's `method` so the
// dashboard activity feed shows where the action came from.
//
// On an illegal move (e.g. double-ack, ack-of-closed) Apply returns an error
// wrapping comment.ErrInvalidTransition and writes nothing — the record state is
// left unchanged. The caller (receiver) turns that into a user-facing refusal in
// the chat reply.
//
// The write reproduces the CRUD createHandler sequence for the comment doc:
// Validate → TransformWrite → GuardWrite → host.DB().Write("comment") →
// AfterCreate, by casting the comment plugin to the optional hook interfaces.
func Apply(ctx context.Context, host plugins.Host, recordUID, action, actor, channel string) (newState string, err error) {
	if !validActions[action] {
		return "", fmt.Errorf("chataction: unsupported action %q", action)
	}
	if recordUID == "" {
		return "", errors.New("chataction: empty record uid")
	}
	if host == nil || host.DB() == nil {
		return "", errors.New("chataction: nil host or DB")
	}

	// 1. Resolve the record in the request's tenant scope. A miss is a hard
	//    error (no comment, no panic).
	rec, err := host.DB().GetOne(ctx, "record", db.Document{"uid": recordUID})
	if err != nil {
		return "", fmt.Errorf("chataction: lookup record %s: %w", recordUID, err)
	}
	currentState, _ := rec["state"].(string)

	// 2. Pre-check the transition against the Plan 05 table directly, so an
	//    illegal move is refused BEFORE any write and surfaces the
	//    ErrInvalidTransition sentinel to the caller. (GuardWrite re-checks the
	//    same table during the write; this early check keeps the refusal path
	//    side-effect-free and gives the caller a clean errors.Is target.)
	if verr := comment.ValidateTransition(currentState, action); verr != nil {
		return "", fmt.Errorf("chataction: %w", verr)
	}

	// 3. Obtain the comment plugin from the host and cast to the hook
	//    interfaces the CRUD createHandler drives.
	cp := host.Plugin("comment")
	if cp == nil {
		return "", errors.New("chataction: comment plugin not available")
	}

	// 4. Build the attributed comment doc. `method` is set explicitly so
	//    TransformWrite preserves the channel attribution (it only fills method
	//    from the JWT claims when unset), mirroring the teams/jira bridge
	//    comments.
	doc := map[string]any{
		"record_uid": recordUID,
		"type":       action,
		"message":    fmt.Sprintf("%s via %s by %s", action, channel, actor),
		"user":       actor,
		"method":     channel,
	}

	// 5. Run the CRUD create sequence: Validate → TransformWrite → GuardWrite →
	//    Write → AfterCreate, exactly as internal/plugins/crud.go createHandler
	//    does, so the Plan-05 guard and the state-application both fire.
	if dm, ok := cp.(plugins.DataModel); ok {
		if verr := dm.Validate(doc); verr != nil {
			return "", fmt.Errorf("chataction: validate comment: %w", verr)
		}
	}
	if wt, ok := cp.(plugins.WriteTransformer); ok {
		if verr := wt.TransformWrite(ctx, doc); verr != nil {
			return "", fmt.Errorf("chataction: transform comment: %w", verr)
		}
	}
	if g, ok := cp.(plugins.WriteGuard); ok {
		if verr := g.GuardWrite(ctx, "", doc, false); verr != nil {
			return "", fmt.Errorf("chataction: %w", verr)
		}
	}

	if _, verr := host.DB().Write(ctx, "comment", []db.Document{doc}, db.WriteOptions{UpdateTime: true}); verr != nil {
		return "", fmt.Errorf("chataction: write comment: %w", verr)
	}

	if hook, ok := cp.(plugins.CreateHook); ok {
		if verr := hook.AfterCreate(ctx, []map[string]any{doc}); verr != nil {
			return "", fmt.Errorf("chataction: apply transition: %w", verr)
		}
	}

	// 6. The new record state is the action's target state (the comment plugin's
	//    AfterCreate just wrote it). The lifecycle trio map 1:1; only the
	//    timed-shelve pair differs and is not exposed on the chat surface.
	return action, nil
}
