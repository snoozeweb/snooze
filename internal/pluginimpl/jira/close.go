// This file holds the JIRA notifier's close path: what happens to the issue
// when the alert it was opened for closes.

package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

var _ plugins.CloseNotifier = (*Plugin)(nil)

// Close dispositions, from the `on_close` action field.
const (
	// closeComment comments on the issue, then applies close_transition when
	// one is configured. The default.
	closeComment = "comment"
	// closeSkip leaves the issue untouched.
	closeSkip = "skip"
)

// refCloseSynced is the handle key recording the close already synced to the
// issue (its epoch), so a retried dispatch of the same close does not comment
// twice.
const refCloseSynced = "close_synced_at"

// closeChannel is the comment `method` the snooze-jira daemon posts under; a
// close arriving through it came from JIRA and is not echoed back.
const closeChannel = "jira"

// NotifyClose implements plugins.CloseNotifier. It comments on the issue this
// action created for the alert — who closed it, or that the source recovered,
// plus the operator's "Resolved:" note — and moves it to close_transition when
// one is configured.
//
// Order and failure handling mirror escalate: the comment first, because it is
// what a reader of the ticket needs; the transition after. Unlike escalate, a
// failed transition IS returned: the dispatcher records it on the alert's
// timeline, which is the only place an operator would notice a ticket left
// open.
func (p *Plugin) NotifyClose(ctx context.Context, rec snoozetypes.Record, payload plugins.NotificationPayload, ev plugins.CloseEvent) error {
	cfg, err := configFromMeta(payload.Meta)
	if err != nil {
		return fmt.Errorf("jira: config: %w", err)
	}
	if cfg.OnClose == closeSkip || strings.EqualFold(ev.Channel, closeChannel) {
		return nil
	}
	action := payload.ActionName()
	key := plugins.NotifyRefString(rec, action, refIssueKey)
	if key == "" {
		return nil
	}
	at := ev.At.Unix()
	if synced, ok := plugins.NotifyRef(rec, action)[refCloseSynced]; ok && refInt64(synced) == at {
		return nil
	}

	status, preview, err := p.addComment(ctx, cfg, key, closeCommentText(rec, ev))
	if err != nil {
		return err
	}
	if status == http.StatusNotFound {
		// The ticket is gone; there is nothing left to close.
		p.warn("jira: close: recorded issue no longer exists", "issue_key", key)
		return nil
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("jira: close comment on %s: HTTP %d: %s", key, status, truncate(preview, 200))
	}
	// The comment landed: record it before the transition, so a transition
	// failure retried later does not repeat the comment.
	plugins.StoreNotifyRef(payload, action,
		plugins.MergeNotifyRef(rec, action, map[string]any{refCloseSynced: at}))

	if cfg.CloseTransition == "" {
		return nil
	}
	return p.transitionOnClose(ctx, cfg, key)
}

// transitionOnClose moves issueKey to cfg.CloseTransition unless it already
// sits in the Done status category. A numeric value is taken as a transition id
// as-is; anything else is matched against the offered transitions' target
// status and name.
func (p *Plugin) transitionOnClose(ctx context.Context, cfg config, issueKey string) error {
	done, err := p.isDone(ctx, cfg, issueKey)
	if err != nil {
		return fmt.Errorf("jira: close: status check on %s: %w", issueKey, err)
	}
	if done {
		return nil
	}
	id := cfg.CloseTransition
	if _, numErr := strconv.Atoi(id); numErr != nil {
		if id, err = p.transitionID(ctx, cfg, issueKey, cfg.CloseTransition); err != nil {
			return fmt.Errorf("jira: close: transition lookup on %s: %w", issueKey, err)
		}
		if id == "" {
			return fmt.Errorf("jira: close: %s offers no transition to %q", issueKey, cfg.CloseTransition)
		}
	}
	body, err := json.Marshal(map[string]any{"transition": map[string]any{"id": id}})
	if err != nil {
		return err
	}
	status, preview, err := p.do(ctx, cfg, http.MethodPost,
		cfg.JiraURL+"/rest/api/3/issue/"+issueKey+"/transitions", body, "transition")
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("jira: close transition on %s: HTTP %d: %s", issueKey, status, truncate(preview, 200))
	}
	return nil
}

// closeCommentText renders the close comment.
func closeCommentText(rec snoozetypes.Record, ev plugins.CloseEvent) string {
	var b strings.Builder
	when := ev.At.UTC().Format(time.RFC3339)
	switch {
	case ev.Actor != "":
		fmt.Fprintf(&b, "Snooze alert closed by %s at %s.", ev.Actor, when)
	case ev.Reason != "":
		fmt.Fprintf(&b, "Snooze alert closed automatically at %s (%s).", when, ev.Reason)
	default:
		fmt.Fprintf(&b, "Snooze alert closed automatically at %s.", when)
	}
	if rec.Host != "" || rec.Message != "" {
		fmt.Fprintf(&b, "\nAlert: %s - %s", rec.Host, rec.Message)
	}
	if ev.Resolution != "" {
		fmt.Fprintf(&b, "\nResolution: %s", ev.Resolution)
	}
	return b.String()
}

// refInt64 reads a number out of a handle, whatever shape the driver decoded
// it into.
func refInt64(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case float64:
		return int64(n)
	case json.Number:
		i, _ := n.Int64()
		return i
	}
	return 0
}
