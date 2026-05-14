package jira

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
)

// envelope is the wire shape of one /alert payload entry. The bulk of the
// fields shadow the JIRA config so operators can override per-alert via the
// webhook action payload in snooze-server.
type envelope struct {
	ProjectKey       string         `json:"project_key"`
	IssueType        string         `json:"issue_type"`
	IssueTypeID      jsonString     `json:"issue_type_id"`
	Priority         string         `json:"priority"`
	Labels           []string       `json:"labels"`
	Assignee         string         `json:"assignee"`
	Reporter         string         `json:"reporter"`
	InitialStatus    string         `json:"initial_status"`
	ExtraFields      map[string]any `json:"extra_fields"`
	CustomFields     map[string]any `json:"custom_fields"`
	Alert            recordSummary  `json:"alert"`
	Message          string         `json:"message"`
	NotificationName string         `json:"-"` // populated from alert.notification_from.name
	NotificationMsg  string         `json:"-"` // populated from alert.notification_from.message
}

// jsonString accepts either a JSON string or a JSON number and stores the
// canonical string form. This mirrors the Python plugin's tolerance for
// `issue_type_id: 10001` (int) in YAML/JSON.
type jsonString string

// UnmarshalJSON tolerates strings and JSON numbers.
func (s *jsonString) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		*s = ""
		return nil
	}
	if data[0] == '"' {
		var v string
		if err := jsonUnmarshalString(data, &v); err != nil {
			return err
		}
		*s = jsonString(v)
		return nil
	}
	// Numbers and any other primitive: keep raw text trimmed of quotes.
	*s = jsonString(strings.TrimSpace(strings.Trim(string(data), `"`)))
	return nil
}

// forwarder turns inbound envelopes into JIRA issue/comment calls. It owns
// a user-resolution cache shared across calls.
type forwarder struct {
	cfg    Config
	jira   *Client
	logger *slog.Logger

	userMu sync.Mutex
	users  map[string]string // email → accountId. "" means lookup failed.
}

// newForwarder constructs a forwarder bound to cfg and jira.
func newForwarder(cfg Config, jira *Client, logger *slog.Logger) *forwarder {
	if logger == nil {
		logger = slog.Default()
	}
	return &forwarder{
		cfg:    cfg,
		jira:   jira,
		logger: logger,
		users:  map[string]string{},
	}
}

// alertResult is the per-record payload returned by handleEnvelopes. The
// daemon writes it back to snooze-server as the webhook response so the
// next escalation of the same alert routes to the existing JIRA issue.
type alertResult struct {
	IssueKey string `json:"issue_key"`
}

// handleEnvelopes processes a batch of /alert payloads. The map is keyed by
// the alert's `hash`; entries without a hash are processed but skipped from
// the response (snooze-server keys the response by hash).
//
// Errors against individual envelopes are logged and skipped; we never abort
// the whole batch on a single failure so partial success is still possible.
func (f *forwarder) handleEnvelopes(ctx context.Context, envs []envelope, actionName string) map[string]alertResult {
	out := map[string]alertResult{}
	limit := f.cfg.MessageLimit
	if limit <= 0 {
		limit = len(envs)
	}
	for i, env := range envs {
		if i >= limit {
			f.logger.Warn("jira: message_limit reached, dropping remainder",
				slog.Int("total", len(envs)),
				slog.Int("limit", limit))
			break
		}
		record := env.Alert
		if record == nil {
			record = recordSummary{}
		}
		recordHash := strField(record, "hash", "")
		populateNotification(&env, record)

		existing := findExistingIssue(record, actionName)
		if existing != "" {
			f.updateExisting(ctx, existing, record, env)
			if recordHash != "" {
				out[recordHash] = alertResult{IssueKey: existing}
			}
			continue
		}

		issueKey, err := f.createNew(ctx, env, record, recordHash)
		if err != nil {
			f.logger.Error("jira: create issue failed",
				slog.String("record_hash", recordHash),
				slog.Any("err", err))
			continue
		}
		if issueKey != "" && recordHash != "" {
			out[recordHash] = alertResult{IssueKey: issueKey}
		}
	}
	return out
}

// populateNotification flattens record.notification_from into the envelope so
// downstream code doesn't have to keep walking the record map.
func populateNotification(env *envelope, record recordSummary) {
	nf, ok := record["notification_from"].(map[string]any)
	if !ok {
		return
	}
	if v, ok := nf["name"].(string); ok {
		env.NotificationName = v
	}
	if v, ok := nf["message"].(string); ok {
		env.NotificationMsg = v
	}
}

// findExistingIssue walks the record's snooze_webhook_responses and returns
// the JIRA issue key created by a previous invocation of this action, or ""
// when none exists. The shape mirrors the Python `_find_existing_issue`.
func findExistingIssue(record recordSummary, actionName string) string {
	raw, ok := record["snooze_webhook_responses"]
	if !ok {
		return ""
	}
	responses, ok := raw.([]any)
	if !ok {
		return ""
	}
	for _, r := range responses {
		entry, ok := r.(map[string]any)
		if !ok {
			continue
		}
		if actionName != "" {
			if name, _ := entry["action_name"].(string); name != actionName {
				continue
			}
		}
		content, ok := entry["content"].(map[string]any)
		if !ok {
			continue
		}
		if key, ok := content["issue_key"].(string); ok && key != "" {
			return key
		}
	}
	return ""
}

// updateExisting adds a comment to a pre-existing issue and optionally
// reopens it when ReopenClosed is set.
func (f *forwarder) updateExisting(ctx context.Context, issueKey string, record recordSummary, env envelope) {
	comment := buildComment(record, env)
	if err := f.jira.AddComment(ctx, issueKey, comment); err != nil {
		f.logger.Error("jira: add comment failed",
			slog.String("issue_key", issueKey),
			slog.Any("err", err))
		return
	}
	f.logger.Info("jira: commented on existing issue", slog.String("issue_key", issueKey))
	if f.cfg.ReopenClosed {
		f.reopenIfClosed(ctx, issueKey)
	}
}

// buildComment renders the re-escalation comment body. Kept plain text — JIRA
// wraps it in ADF inside AddComment.
func buildComment(record recordSummary, env envelope) string {
	var b strings.Builder
	timestamp := strField(record, "timestamp", "")
	if timestamp != "" {
		fmt.Fprintf(&b, "Re-escalation at %s\n", timestamp)
	} else {
		b.WriteString("Re-escalation\n")
	}
	if env.NotificationName != "" {
		fmt.Fprintf(&b, "From %s\n", env.NotificationName)
		if env.NotificationMsg != "" {
			fmt.Fprintf(&b, "%s\n", env.NotificationMsg)
		}
	}
	fmt.Fprintf(&b, "Host: %s\n", strField(record, "host", "Unknown"))
	fmt.Fprintf(&b, "Severity: %s\n", strField(record, "severity", "Unknown"))
	fmt.Fprintf(&b, "Message: %s", strField(record, "message", "No message"))
	if env.Message != "" {
		fmt.Fprintf(&b, "\nCustom message: %s", env.Message)
	}
	return b.String()
}

// reopenIfClosed transitions issueKey back to cfg.ReopenStatusName when it
// currently sits in the Done status category. Errors are logged, not
// returned — the comment was already added and we don't want to fail the
// outer webhook because of a transition glitch.
func (f *forwarder) reopenIfClosed(ctx context.Context, issueKey string) {
	issue, err := f.jira.GetIssue(ctx, issueKey)
	if err != nil {
		f.logger.Warn("jira: get issue for reopen failed",
			slog.String("issue_key", issueKey), slog.Any("err", err))
		return
	}
	if !strings.EqualFold(issue.Fields.Status.Category.Key, "done") {
		return
	}
	if err := f.transitionToStatus(ctx, issueKey, f.cfg.ReopenStatusName,
		"Reopened by Snooze due to re-escalation"); err != nil {
		f.logger.Warn("jira: reopen failed",
			slog.String("issue_key", issueKey),
			slog.String("target", f.cfg.ReopenStatusName),
			slog.Any("err", err))
	}
}

// createNew creates a brand-new JIRA issue and applies the configured
// initial-status transition when applicable.
func (f *forwarder) createNew(ctx context.Context, env envelope, record recordSummary, recordHash string) (string, error) {
	projectKey := env.ProjectKey
	if projectKey == "" {
		projectKey = f.cfg.ProjectKey
	}
	if projectKey == "" {
		return "", errors.New("missing project_key")
	}

	issueType, issueTypeID := resolveIssueType(env, f.cfg)
	priority := resolvePriority(env, record, f.cfg)
	labels := env.Labels
	if labels == nil {
		labels = f.cfg.Labels
	}

	// Build the merged extra fields (config defaults + payload override).
	extra := map[string]any{}
	for k, v := range f.cfg.ExtraFields {
		extra[k] = v
	}
	for k, v := range env.ExtraFields {
		extra[k] = v
	}
	customFields := map[string]any{}
	for k, v := range f.cfg.CustomFields {
		customFields[k] = v
	}
	for k, v := range env.CustomFields {
		customFields[k] = v
	}

	// Assignee / reporter: resolve email → accountId on demand.
	assignee := chooseString(env.Assignee, f.cfg.Assignee)
	reporter := chooseString(env.Reporter, f.cfg.Reporter)
	if assignee != "" {
		if uf := f.resolveUserField(ctx, assignee); uf != nil {
			extra["assignee"] = uf
		}
	}
	if reporter != "" {
		if uf := f.resolveUserField(ctx, reporter); uf != nil {
			extra["reporter"] = uf
		}
	}
	for k, v := range customFields {
		extra[k] = v
	}

	if f.cfg.AlertHashCustomField != "" && recordHash != "" {
		link := f.cfg.SnoozeURL + "/web/?#/record?tab=All&s=hash%3D" + recordHash
		extra[f.cfg.AlertHashCustomField] = link
	}

	summary := f.formatSummary(record)
	description := f.formatDescription(record)

	if env.Message != "" {
		description = appendStrongLine(description, "Custom message", env.Message)
	}
	if env.NotificationName != "" {
		text := "Notified by " + env.NotificationName
		if env.NotificationMsg != "" {
			text += ": " + env.NotificationMsg
		}
		description = appendPlainLine(description, text)
	}

	req := CreateIssueRequest{
		ProjectKey:  projectKey,
		IssueType:   issueType,
		IssueTypeID: issueTypeID,
		Summary:     summary,
		Description: description,
		Priority:    priority,
		Labels:      labels,
		ExtraFields: extra,
	}
	resp, err := f.jira.CreateIssue(ctx, req)
	if err != nil {
		return "", err
	}
	f.logger.Info("jira: created issue",
		slog.String("issue_key", resp.Key),
		slog.String("record_hash", recordHash))

	initial := chooseString(env.InitialStatus, f.cfg.InitialStatus)
	if resp.Key != "" && initial != "" {
		if err := f.transitionToStatus(ctx, resp.Key, initial, ""); err != nil {
			f.logger.Warn("jira: initial transition failed",
				slog.String("issue_key", resp.Key),
				slog.String("target", initial),
				slog.Any("err", err))
		}
	}
	return resp.Key, nil
}

// resolveIssueType picks issue type / id with the four-step precedence the
// Python plugin documents (payload id > payload name > config id > config
// name). Returns (name, id) — only one of them is populated.
func resolveIssueType(env envelope, cfg Config) (name, id string) {
	id = cfg.IssueTypeID
	name = cfg.IssueType
	if env.IssueType != "" {
		name = env.IssueType
		id = ""
	}
	if string(env.IssueTypeID) != "" {
		id = string(env.IssueTypeID)
	}
	return
}

// resolvePriority picks the priority with payload > severity-mapping > default.
func resolvePriority(env envelope, record recordSummary, cfg Config) string {
	if env.Priority != "" {
		return env.Priority
	}
	severity := strings.ToLower(strField(record, "severity", ""))
	if mapped, ok := cfg.PriorityMapping[severity]; ok && mapped != "" {
		return mapped
	}
	return cfg.Priority
}

// chooseString returns first when non-empty, second otherwise.
func chooseString(first, second string) string {
	if first != "" {
		return first
	}
	return second
}

// resolveUserField turns an assignee/reporter setting into the JIRA fields
// shape `{id: <accountId>}`. Account IDs are passed through; email addresses
// are resolved via /user/search and cached for the forwarder's lifetime.
// Returns nil when an email cannot be resolved.
func (f *forwarder) resolveUserField(ctx context.Context, value string) map[string]any {
	if !strings.Contains(value, "@") {
		return map[string]any{"id": value}
	}
	f.userMu.Lock()
	cached, ok := f.users[value]
	f.userMu.Unlock()
	if ok {
		if cached == "" {
			return nil
		}
		return map[string]any{"id": cached}
	}
	id, err := f.jira.FindUserByEmail(ctx, value)
	if err != nil {
		f.logger.Warn("jira: user lookup failed",
			slog.String("email", value),
			slog.Any("err", err))
		f.userMu.Lock()
		f.users[value] = ""
		f.userMu.Unlock()
		return nil
	}
	f.userMu.Lock()
	f.users[value] = id
	f.userMu.Unlock()
	if id == "" {
		f.logger.Warn("jira: no user found for email", slog.String("email", value))
		return nil
	}
	return map[string]any{"id": id}
}

// transitionToStatus walks the issue's available transitions and applies the
// one whose destination matches target. Falls back to the first non-Done
// transition, mirroring the Python behaviour.
func (f *forwarder) transitionToStatus(ctx context.Context, issueKey, target, comment string) error {
	transitions, err := f.jira.GetTransitions(ctx, issueKey)
	if err != nil {
		return fmt.Errorf("get transitions: %w", err)
	}
	var picked *TransitionEntry
	for i := range transitions {
		if strings.EqualFold(transitions[i].To.Name, target) {
			picked = &transitions[i]
			break
		}
	}
	if picked == nil {
		for i := range transitions {
			if !strings.EqualFold(transitions[i].To.Category.Key, "done") {
				picked = &transitions[i]
				break
			}
		}
	}
	if picked == nil {
		return fmt.Errorf("no transition to %q available", target)
	}
	if err := f.jira.Transition(ctx, issueKey, picked.ID, comment); err != nil {
		return fmt.Errorf("apply transition %s: %w", picked.ID, err)
	}
	f.logger.Info("jira: transitioned issue",
		slog.String("issue_key", issueKey),
		slog.String("target", target),
		slog.String("transition", picked.Name))
	return nil
}

// templateVars are the variable substitutions supported by summary_template
// and description_template. We use the Python `${var}` syntax via
// strings.NewReplacer rather than text/template so configs ported from the
// Python plugin keep working unmodified.
func templateVars(record recordSummary, snoozeURL string) *strings.Replacer {
	return strings.NewReplacer(
		"${severity}", strField(record, "severity", "Unknown"),
		"${host}", strField(record, "host", "Unknown"),
		"${source}", strField(record, "source", "Unknown"),
		"${process}", strField(record, "process", "Unknown"),
		"${message}", strField(record, "message", "No message"),
		"${timestamp}", strField(record, "timestamp", ""),
		"${hash}", strField(record, "hash", ""),
		"${snooze_url}", snoozeURL,
	)
}

// formatSummary renders cfg.SummaryTemplate against the record and clamps
// the result to 255 characters (JIRA's summary field limit).
func (f *forwarder) formatSummary(record recordSummary) string {
	rendered := templateVars(record, f.cfg.SnoozeURL).Replace(f.cfg.SummaryTemplate)
	if len(rendered) > 255 {
		rendered = rendered[:255]
	}
	return rendered
}

// formatDescription renders cfg.DescriptionTemplate when set, or falls back
// to the canonical rich ADF description.
func (f *forwarder) formatDescription(record recordSummary) ADF {
	if f.cfg.DescriptionTemplate == "" {
		return buildDescriptionADF(record, f.cfg.SnoozeURL)
	}
	rendered := templateVars(record, f.cfg.SnoozeURL).Replace(f.cfg.DescriptionTemplate)
	return textADF(rendered)
}

// jsonUnmarshalString is a tiny json string decoder so we don't have to
// import encoding/json into adf.go just for jsonString.UnmarshalJSON. It
// errors on anything but a valid JSON string.
func jsonUnmarshalString(data []byte, dest *string) error {
	if len(data) < 2 || data[0] != '"' || data[len(data)-1] != '"' {
		return errInvalidJSONString
	}
	// We deliberately don't decode escape sequences — issue-type ids never
	// contain them and adding a real json reader for this would be silly.
	*dest = string(data[1 : len(data)-1])
	return nil
}

var errInvalidJSONString = errors.New("jira: invalid JSON string")
