package plugins

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// CloseEvent describes an alert closing, as told to a notifier that keeps an
// external artifact for it (a ticket, a thread).
type CloseEvent struct {
	// Actor is the login of the operator who closed the alert; "" for an
	// automatic close (the source reported recovery).
	Actor string
	// Channel is the close comment's `method`: the auth method of a human
	// close, or the chat-ops channel a bridge posted it through ("jira",
	// "teams", "mcp"). A notifier skips a close that came from its own
	// system — a ticket resolved in JIRA closing the alert must not comment
	// "closed" back onto that ticket.
	Channel string
	// Reason is the automatic close's own explanation ("Severity critical =>
	// ok"); "" on a human close.
	Reason string
	// Resolution is the text of the latest "Resolved:" timeline comment, when
	// there is one — what the operator recorded as the fix. Filled in by the
	// dispatcher when the caller leaves it empty.
	Resolution string
	// At is when the alert closed. It doubles as the idempotency key a
	// notifier stores in its handle, so a retried dispatch of the same close
	// does not act twice.
	At time.Time
}

// CloseNotifier is the optional refinement a Notifier implements to hear about
// the close of an alert it already delivered.
//
// The notification dispatcher skips closed records (a close is not a page), so
// without this a notifier that opened a ticket never learns the alert it was
// opened for is over, and the ticket stays open forever. The dispatcher calls
// NotifyClose once per action whose `notify_ref_<action>` handle is on the
// record, with the same Meta a Send for that action would get.
//
// NotifyClose runs off the request / ingest path. An error is recorded on the
// alert's timeline and never undoes the close.
type CloseNotifier interface {
	Notifier
	NotifyClose(ctx context.Context, rec snoozetypes.Record, payload NotificationPayload, ev CloseEvent) error
}

// CloseDispatcher is implemented by the notification plugin: fan a close of
// the record recordUID out to every CloseNotifier holding a handle on it.
// It returns immediately; the fan-out is detached and best-effort.
type CloseDispatcher interface {
	DispatchClose(ctx context.Context, recordUID string, ev CloseEvent)
}

// DispatchClose hands a close to the host's notification plugin, when one is
// wired and implements CloseDispatcher. A no-op otherwise, so callers need no
// guard of their own.
func DispatchClose(ctx context.Context, host Host, recordUID string, ev CloseEvent) {
	if host == nil || recordUID == "" {
		return
	}
	if d, ok := host.Plugin("notification").(CloseDispatcher); ok {
		d.DispatchClose(ctx, recordUID, ev)
	}
}

// NotifyRefActions returns the action names that hold a canonical
// `notify_ref_<action>` handle on rec — the actions a close fans out to.
func NotifyRefActions(rec snoozetypes.Record) []string {
	var out []string
	for k, v := range rec.Extra {
		name, ok := strings.CutPrefix(k, notifyRefPrefix)
		if !ok || name == "" {
			continue
		}
		if ref, ok := v.(map[string]any); ok && len(ref) > 0 {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}
