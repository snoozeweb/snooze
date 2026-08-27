// This file holds the JIRA notifier's re-escalation path: what happens when an
// alert that already has an issue fires again.
//
// The rule the whole file exists to enforce: ONE alert, ONE ticket. An operator
// who acknowledges a JIRA issue, walks away, and comes back to a re-escalated
// alert must find the same ticket with a new comment on it — not a second
// ticket competing with the first for their attention, and not a silent
// re-notification that never reached JIRA at all.

package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/snoozeweb/snooze/internal/jiraadf"
	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// refIssueKey is the key under which the created issue's key is persisted on
// the alert (inside the notifier handle — see plugins.NotifyRef).
const refIssueKey = "issue_key"

// Escalation dispositions, from the `on_escalation` action field.
const (
	// escalateComment adds a comment to the existing issue and leaves its
	// status alone.
	escalateComment = "comment"
	// escalateReopen does the same, and additionally transitions a Done issue
	// back to the configured reopen status. The default: an alert that is
	// escalating again is by definition not resolved.
	escalateReopen = "reopen"
	// escalateNew creates a second issue, linked to the first. For teams whose
	// process wants one ticket per occurrence.
	escalateNew = "new"
	// escalateSkip does nothing on JIRA. For teams who treat the ticket as a
	// create-only record and escalate through some other channel.
	escalateSkip = "skip"
)

// defaultReopenStatus is the status a Done issue is transitioned back to when
// on_escalation is "reopen" and the action names no status of its own. Matches
// the snooze-jira daemon's default.
const defaultReopenStatus = "To Do"

// defaultLinkType is the issue-link type used when on_escalation is "new".
const defaultLinkType = "Relates"

// escalate updates the issue an earlier delivery created for this alert.
//
// Ordering matters: the comment goes first, because it is the part an operator
// actually reads and the part that must survive a partial failure. A priority
// bump or a reopen transition that fails is logged and swallowed — the
// escalation has already been recorded on the ticket, and failing the whole
// notification over a workflow quirk (a transition the project does not offer,
// a priority scheme that changed) would lose that.
func (p *Plugin) escalate(
	ctx context.Context,
	cfg config,
	rec snoozetypes.Record,
	payload plugins.NotificationPayload,
	issueKey string,
) error {
	switch cfg.OnEscalation {
	case escalateSkip:
		return nil
	case escalateNew:
		return p.createLinked(ctx, cfg, rec, issueKey)
	}

	status, preview, err := p.addComment(ctx, cfg, issueKey, p.escalationComment(cfg, rec, payload))
	if err != nil {
		return err
	}
	if status == http.StatusNotFound {
		// The ticket is gone (deleted, or moved to a project we cannot see).
		// The handle is stale, so fall back to a create and overwrite it —
		// otherwise this alert would never reach JIRA again.
		p.warn("jira: recorded issue no longer exists, creating a new one",
			"issue_key", issueKey)
		return p.create(ctx, cfg, rec, payload)
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("jira: comment on %s: HTTP %d: %s", issueKey, status, truncate(preview, 200))
	}

	// Only raise the priority when severity actually rose. Re-escalating a
	// warning must not silently promote a ticket an operator downgraded.
	if payload.Escalation.SeverityRose() {
		p.raisePriority(ctx, cfg, rec, issueKey)
	}
	if cfg.OnEscalation == escalateReopen {
		p.reopenIfDone(ctx, cfg, issueKey)
	}
	return nil
}

// escalationComment renders the comment body via the builder shared with the
// snooze-jira daemon, so both modes leave the same audit trail.
func (p *Plugin) escalationComment(cfg config, rec snoozetypes.Record, payload plugins.NotificationPayload) string {
	esc := payload.Escalation
	custom := ""
	if strings.TrimSpace(cfg.EscalationComment) != "" {
		if rendered, err := renderTemplate(cfg.EscalationComment, rec); err == nil {
			custom = rendered
		} else {
			p.warn("jira: render escalation_comment template failed, omitting it", "error", err)
		}
	}
	ts := ""
	if !rec.Timestamp.IsZero() {
		ts = rec.Timestamp.Format(time.RFC3339)
	}
	notificationName, _ := payload.Meta["notification_name"].(string)
	return jiraadf.BuildEscalationComment(jiraadf.RecordSummary{
		"timestamp": ts,
		"host":      rec.Host,
		"severity":  rec.Severity,
		"message":   rec.Message,
	}, jiraadf.EscalationComment{
		Ordinal:          esc.Ordinal(),
		Reason:           esc.Reason,
		Actor:            esc.Actor,
		NotificationName: notificationName,
		CustomMessage:    custom,
	})
}

// addComment POSTs a plain-text comment, wrapped in the single-paragraph ADF
// document the v3 API expects.
func (p *Plugin) addComment(ctx context.Context, cfg config, issueKey, text string) (int, []byte, error) {
	body, err := json.Marshal(map[string]any{"body": jiraadf.TextADF(text)})
	if err != nil {
		return 0, nil, fmt.Errorf("jira: marshal comment: %w", err)
	}
	return p.do(ctx, cfg, http.MethodPost,
		cfg.JiraURL+"/rest/api/3/issue/"+issueKey+"/comment", body, "comment")
}

// raisePriority PUTs the severity-derived priority onto the existing issue,
// reusing the live-scheme resolver so the value is one this JIRA site actually
// accepts (priority names are localised and project-specific).
//
// Best-effort: a project with no priority field, or a scheme that rejects the
// value, must not fail an escalation whose comment already landed.
func (p *Plugin) raisePriority(ctx context.Context, cfg config, rec snoozetypes.Record, issueKey string) {
	field, name := p.priorityField(ctx, cfg, rec.Severity)
	fields := map[string]any{}
	setPriority(fields, field, name)
	if len(fields) == 0 {
		return
	}
	body, err := json.Marshal(map[string]any{"fields": fields})
	if err != nil {
		return
	}
	status, preview, err := p.do(ctx, cfg, http.MethodPut,
		cfg.JiraURL+"/rest/api/3/issue/"+issueKey, body, "priority update")
	if err != nil {
		p.warn("jira: priority bump failed", "issue_key", issueKey, "error", err)
		return
	}
	if status < 200 || status >= 300 {
		p.warn("jira: priority bump rejected", "issue_key", issueKey,
			"status", status, "response", truncate(preview, 200))
	}
}

// reopenIfDone transitions issueKey back to the configured reopen status when
// it currently sits in the Done status category. An issue already in progress
// is left alone — an operator working it does not want their status reset on
// every escalation.
//
// Best-effort for the same reason as raisePriority.
func (p *Plugin) reopenIfDone(ctx context.Context, cfg config, issueKey string) {
	done, err := p.isDone(ctx, cfg, issueKey)
	if err != nil {
		p.warn("jira: reopen check failed", "issue_key", issueKey, "error", err)
		return
	}
	if !done {
		return
	}
	id, err := p.transitionID(ctx, cfg, issueKey, cfg.ReopenStatus)
	if err != nil {
		p.warn("jira: reopen transition lookup failed", "issue_key", issueKey,
			"target", cfg.ReopenStatus, "error", err)
		return
	}
	if id == "" {
		p.warn("jira: no transition to the configured reopen status",
			"issue_key", issueKey, "target", cfg.ReopenStatus)
		return
	}
	body, err := json.Marshal(map[string]any{"transition": map[string]any{"id": id}})
	if err != nil {
		return
	}
	status, preview, err := p.do(ctx, cfg, http.MethodPost,
		cfg.JiraURL+"/rest/api/3/issue/"+issueKey+"/transitions", body, "transition")
	if err != nil {
		p.warn("jira: reopen failed", "issue_key", issueKey, "error", err)
		return
	}
	if status < 200 || status >= 300 {
		p.warn("jira: reopen rejected", "issue_key", issueKey,
			"status", status, "response", truncate(preview, 200))
	}
}

// isDone reports whether the issue's current status is in JIRA's "done"
// status category.
func (p *Plugin) isDone(ctx context.Context, cfg config, issueKey string) (bool, error) {
	status, preview, err := p.do(ctx, cfg, http.MethodGet,
		cfg.JiraURL+"/rest/api/3/issue/"+issueKey+"?fields=status", nil, "get issue")
	if err != nil {
		return false, err
	}
	if status < 200 || status >= 300 {
		return false, fmt.Errorf("HTTP %d: %s", status, truncate(preview, 200))
	}
	var issue struct {
		Fields struct {
			Status struct {
				StatusCategory struct {
					Key string `json:"key"`
				} `json:"statusCategory"`
			} `json:"status"`
		} `json:"fields"`
	}
	if err := json.Unmarshal(preview, &issue); err != nil {
		return false, err
	}
	return issue.Fields.Status.StatusCategory.Key == "done", nil
}

// transitionID resolves the id of the transition whose target status matches
// target (case-insensitive), or "" when the issue's workflow offers none.
func (p *Plugin) transitionID(ctx context.Context, cfg config, issueKey, target string) (string, error) {
	status, preview, err := p.do(ctx, cfg, http.MethodGet,
		cfg.JiraURL+"/rest/api/3/issue/"+issueKey+"/transitions", nil, "get transitions")
	if err != nil {
		return "", err
	}
	if status < 200 || status >= 300 {
		return "", fmt.Errorf("HTTP %d: %s", status, truncate(preview, 200))
	}
	var resp struct {
		Transitions []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			To   struct {
				Name string `json:"name"`
			} `json:"to"`
		} `json:"transitions"`
	}
	if err := json.Unmarshal(preview, &resp); err != nil {
		return "", err
	}
	for _, tr := range resp.Transitions {
		if strings.EqualFold(tr.To.Name, target) || strings.EqualFold(tr.Name, target) {
			return tr.ID, nil
		}
	}
	return "", nil
}

// createLinked implements on_escalation="new": create a second issue and link
// it to the one the previous delivery created, so an operator landing on either
// ticket can still see the alert's history.
//
// The handle is deliberately NOT overwritten with the new key: the first issue
// stays the alert's anchor, so a third escalation links to that same original
// rather than building a chain nobody can follow.
func (p *Plugin) createLinked(
	ctx context.Context,
	cfg config,
	rec snoozetypes.Record,
	previousKey string,
) error {
	key, err := p.createIssue(ctx, cfg, rec)
	if err != nil {
		return err
	}
	if key == "" {
		// Created, but we cannot name it, so there is nothing to link.
		return nil
	}
	p.linkIssues(ctx, cfg, key, previousKey)
	return nil
}

// linkIssues creates a typed link between the follow-up issue and the alert's
// original one.
//
// Best-effort: losing the link is much better than losing the ticket, and a
// project can have the link type renamed or issue linking disabled outright.
func (p *Plugin) linkIssues(ctx context.Context, cfg config, inward, outward string) {
	body, err := json.Marshal(map[string]any{
		"type":         map[string]any{"name": cfg.LinkType},
		"inwardIssue":  map[string]any{"key": inward},
		"outwardIssue": map[string]any{"key": outward},
	})
	if err != nil {
		return
	}
	status, preview, err := p.do(ctx, cfg, http.MethodPost,
		cfg.JiraURL+"/rest/api/3/issueLink", body, "issue link")
	if err != nil {
		p.warn("jira: issue link failed", "inward", inward, "outward", outward, "error", err)
		return
	}
	if status < 200 || status >= 300 {
		p.warn("jira: issue link rejected", "inward", inward, "outward", outward,
			"link_type", cfg.LinkType, "status", status, "response", truncate(preview, 200))
	}
}

// do issues one authenticated request and returns its status plus a bounded
// body preview. what names the operation for error wrapping.
func (p *Plugin) do(ctx context.Context, cfg config, method, url string, body []byte, what string) (int, []byte, error) {
	req, err := p.newRequest(ctx, cfg, method, url, body)
	if err != nil {
		return 0, nil, fmt.Errorf("jira: build %s request: %w", what, err)
	}
	resp, err := p.newClient(cfg.Timeout).Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("jira: %s request: %w", what, err)
	}
	defer resp.Body.Close() //nolint:errcheck
	preview, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	return resp.StatusCode, preview, nil
}
