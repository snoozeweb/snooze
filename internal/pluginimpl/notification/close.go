package notification

import (
	"context"
	"fmt"
	"strings"

	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

var _ plugins.CloseDispatcher = (*Plugin)(nil)

// resolvedPrefix marks the timeline comment an operator (or the snooze skill)
// writes to record what fixed the alert; its text rides into the close event.
const resolvedPrefix = "Resolved:"

// DispatchClose implements plugins.CloseDispatcher: it tells every
// CloseNotifier holding a handle on the record that the alert closed. Detached
// from the caller — the close has already landed and must not wait on JIRA.
func (p *Plugin) DispatchClose(ctx context.Context, recordUID string, ev plugins.CloseEvent) {
	if p.host == nil || p.host.DB() == nil || recordUID == "" {
		return
	}
	// WithoutCancel keeps the caller's values (the tenant scope the reads and
	// writes need) but not its deadline: the request has long returned by the
	// time JIRA answers.
	dctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), notifierSendTimeout)
	go func() {
		defer cancel()
		p.dispatchClose(dctx, recordUID, ev)
	}()
}

// dispatchClose is DispatchClose's synchronous body. For each action with a
// `notify_ref_<action>` handle on the record whose notifier implements
// plugins.CloseNotifier, it builds the payload a Send for that action would get
// and calls NotifyClose. A failure is logged and written to the alert's
// timeline — it never undoes the close.
func (p *Plugin) dispatchClose(ctx context.Context, recordUID string, ev plugins.CloseEvent) {
	doc, err := p.host.DB().GetOne(ctx, recordCollectionName, db.Document{"uid": recordUID})
	if err != nil {
		p.warn("notification: close: record lookup failed", "uid", recordUID, "error", err)
		return
	}
	rec, err := snoozetypes.RecordFromDocument(doc)
	if err != nil {
		p.warn("notification: close: record decode failed", "uid", recordUID, "error", err)
		return
	}
	actions := plugins.NotifyRefActions(rec)
	if len(actions) == 0 {
		return
	}
	if ev.Resolution == "" {
		ev.Resolution = p.latestResolution(ctx, recordUID)
	}
	notificationName := ""
	if from, ok := rec.Extra["notification_from"].(map[string]any); ok {
		notificationName, _ = from["name"].(string)
	}
	for _, name := range actions {
		ad, ok := p.lookupAction(ctx, name)
		if !ok || ad.Action.Selected == "" {
			continue
		}
		cn, ok := p.host.Plugin(ad.Action.Selected).(plugins.CloseNotifier)
		if !ok {
			continue
		}
		payload := plugins.NotificationPayload{
			Template: ad.Action.Selected,
			Meta:     metaFromSubcontent(ad.Action.Subcontent, Entry{Name: notificationName}, ad.Name),
			Inject:   p.injectFunc(ctx, rec),
		}
		if err := cn.NotifyClose(ctx, rec, payload, ev); err != nil {
			p.warn("notification: close sync failed", "uid", recordUID, "action", name, "error", err)
			p.closeFailureComment(ctx, recordUID, name, err)
		}
	}
}

// latestResolution returns the text after "Resolved:" of the newest timeline
// comment that starts with it, "" when there is none.
func (p *Plugin) latestResolution(ctx context.Context, recordUID string) string {
	docs, _, err := p.host.DB().Search(ctx, "comment", condition.Equals("record_uid", recordUID), db.Page{})
	if err != nil {
		return ""
	}
	best, bestAt := "", int64(-1)
	for _, d := range docs {
		msg, _ := d["message"].(string)
		rest, ok := strings.CutPrefix(strings.TrimSpace(msg), resolvedPrefix)
		if !ok {
			continue
		}
		if at := docEpoch(d["date_epoch"]); at >= bestAt {
			best, bestAt = strings.TrimSpace(rest), at
		}
	}
	return best
}

// closeFailureComment writes a system comment on the alert so the operator
// sees that the external ticket was not updated. Direct to the driver, like
// the aggregaterule lifecycle comments, with the comment_count bump alongside.
func (p *Plugin) closeFailureComment(ctx context.Context, recordUID, action string, cause error) {
	doc := db.Document{
		"record_uid": recordUID,
		"type":       "comment",
		"message":    fmt.Sprintf("Close not synced to action %q: %v", action, cause),
		"auto":       true,
	}
	if _, err := p.host.DB().Write(ctx, "comment", []db.Document{doc}, db.WriteOptions{UpdateTime: true}); err != nil {
		p.warn("notification: close: write failure comment", "uid", recordUID, "error", err)
		return
	}
	rec, err := p.host.DB().GetOne(ctx, recordCollectionName, db.Document{"uid": recordUID})
	if err != nil {
		return
	}
	patch := db.Document{"comment_count": docEpoch(rec["comment_count"]) + 1}
	if err := p.host.DB().UpdateOne(ctx, recordCollectionName, recordUID, patch, false); err != nil {
		p.warn("notification: close: bump comment_count", "uid", recordUID, "error", err)
	}
}

func (p *Plugin) warn(msg string, args ...any) {
	if lg := p.logger(); lg != nil {
		lg.Warn(msg, args...)
	}
}

// docEpoch reads an integer out of a document, tolerating the int / int64 /
// float64 shapes the drivers decode numbers into.
func docEpoch(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case int32:
		return int64(n)
	case float64:
		return int64(n)
	}
	return 0
}
