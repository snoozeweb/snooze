package jira

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/snoozeweb/snooze/internal/jiraadf"
	"github.com/snoozeweb/snooze/internal/jirapriority"
)

// envelope is the wire shape of one /alert payload entry. The bulk of the
// fields shadow the JIRA config so operators can override per-alert via the
// webhook action payload in snooze-server.
type envelope struct {
	ProjectKey       string                `json:"project_key"`
	IssueType        string                `json:"issue_type"`
	IssueTypeID      jsonString            `json:"issue_type_id"`
	Priority         string                `json:"priority"`
	Summary          string                `json:"summary"`
	SummaryTemplate  string                `json:"summary_template"`
	Labels           []string              `json:"labels"`
	Assignee         string                `json:"assignee"`
	Reporter         string                `json:"reporter"`
	InitialStatus    string                `json:"initial_status"`
	ExtraFields      map[string]any        `json:"extra_fields"`
	CustomFields     map[string]any        `json:"custom_fields"`
	Alert            jiraadf.RecordSummary `json:"alert"`
	Message          string                `json:"message"`
	NotificationName string                `json:"-"` // populated from alert.notification_from.name
	NotificationMsg  string                `json:"-"` // populated from alert.notification_from.message
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

	// priorities caches each (project, issue type) priority scheme so
	// severity → priority resolution costs one request per project rather
	// than one per alert.
	priorities *jirapriority.Cache
}

// newForwarder constructs a forwarder bound to cfg and jira.
func newForwarder(cfg Config, jira *Client, logger *slog.Logger) *forwarder {
	if logger == nil {
		logger = slog.Default()
	}
	return &forwarder{
		cfg:        cfg,
		jira:       jira,
		logger:     logger,
		users:      map[string]string{},
		priorities: jirapriority.NewCache(cfg.PriorityCacheTTL),
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
			record = jiraadf.RecordSummary{}
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
func populateNotification(env *envelope, record jiraadf.RecordSummary) {
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

// findExistingIssue returns the JIRA issue key a previous invocation of this
// action created for the alert, or "" when there is none — in which case the
// caller opens a new issue.
//
// Three storage shapes are consulted, newest convention first:
//
//  1. `notify_ref_<action>.issue_key` — the canonical handle a notifier stores
//     via plugins.StoreNotifyRef.
//  2. `response_<action>.issue_key` — what the Go webhook plugin's
//     `inject_response` writes when this daemon is driven through a webhook
//     action.
//  3. `snooze_webhook_responses[].content.issue_key` — the Snooze 1.x array,
//     kept for records migrated from a Python deployment.
//
// Shapes 1 and 2 matter because NOTHING in the Go server ever wrote shape 3:
// before they were consulted here, a Go-only deployment found no handle and
// this daemon opened a fresh ticket on every single re-escalation — the very
// duplication it exists to prevent.
//
// Two things this function must survive, both observed in production:
//
//   - The handle can sit one level deeper than shape 2 describes, under the
//     alert hash, because this daemon answers `{"<hash>": {"issue_key": …}}`
//     and older servers stamped that envelope verbatim. issueKeyFrom reads
//     through it.
//   - actionName can be empty, when neither the header nor the query
//     parameter reached us. Then no action-specific field name is knowable
//     and we fall back to any `notify_ref_*` / `response_*` field carrying an
//     issue key: commenting on the wrong action's ticket is recoverable,
//     opening a duplicate on every escalation is what we are here to stop.
func findExistingIssue(record jiraadf.RecordSummary, actionName string) string {
	recordHash := strField(record, "hash", "")
	if actionName != "" {
		for _, prefix := range []string{notifyRefPrefix, responsePrefix} {
			if key := issueKeyFrom(record[prefix+actionName], recordHash); key != "" {
				return key
			}
		}
	} else if key := anyStoredIssueKey(record, recordHash); key != "" {
		return key
	}
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

// Record field prefixes that can hold this daemon's handle. `notify_ref_` is
// written by plugins.StoreNotifyRef, `response_` by the webhook notifier's
// inject_response.
const (
	notifyRefPrefix = "notify_ref_"
	responsePrefix  = "response_"
)

// anyStoredIssueKey scans every handle field on the record for an issue key,
// for the case where the action name is unknown. Fields are visited in a
// stable order — `notify_ref_*` before `response_*`, alphabetical within each
// group — so a record carrying two handles keeps commenting on the same
// ticket instead of alternating between them run to run.
func anyStoredIssueKey(record jiraadf.RecordSummary, recordHash string) string {
	for _, prefix := range []string{notifyRefPrefix, responsePrefix} {
		fields := make([]string, 0, len(record))
		for field := range record {
			if strings.HasPrefix(field, prefix) {
				fields = append(fields, field)
			}
		}
		sort.Strings(fields)
		for _, field := range fields {
			if key := issueKeyFrom(record[field], recordHash); key != "" {
				return key
			}
		}
	}
	return ""
}

// issueKeyFrom pulls an issue key out of one handle field, tolerating the
// batch envelope this daemon's own /alert response uses.
//
// Accepted shapes:
//
//	{"issue_key": "CG-1"}                     — the handle itself
//	{"<recordHash>": {"issue_key": "CG-1"}}   — the batch envelope
//
// A single-entry envelope keyed by some other hash is deliberately ignored:
// its issue key belongs to a different alert.
func issueKeyFrom(value any, recordHash string) string {
	ref, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	if key, _ := ref["issue_key"].(string); key != "" {
		return key
	}
	if recordHash == "" {
		return ""
	}
	inner, ok := ref[recordHash].(map[string]any)
	if !ok {
		return ""
	}
	key, _ := inner["issue_key"].(string)
	return key
}

// updateExisting adds a comment to a pre-existing issue and optionally
// reopens it when ReopenClosed is set.
func (f *forwarder) updateExisting(ctx context.Context, issueKey string, record jiraadf.RecordSummary, env envelope) {
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

// buildComment renders the re-escalation comment body, delegating to the
// builder shared with the in-process jira notifier so both modes leave the same
// audit trail on the same ticket. The escalation ordinal / reason / actor come
// off the alert itself (the server stamps them; see plugins.EscalationFrom), so
// a comment says which escalation it is and why.
func buildComment(record jiraadf.RecordSummary, env envelope) string {
	return jiraadf.BuildEscalationComment(record, jiraadf.EscalationComment{
		Ordinal:          escalationOrdinal(record),
		Reason:           strField(record, "escalation_reason", ""),
		Actor:            strField(record, "escalation_actor", ""),
		NotificationName: env.NotificationName,
		NotificationMsg:  env.NotificationMsg,
		CustomMessage:    env.Message,
	})
}

// escalationOrdinal renders the alert's escalation_count as "#N", or "" when
// the field is absent (an alert from a server that predates the escalation
// bookkeeping, which must keep producing the original header).
func escalationOrdinal(record jiraadf.RecordSummary) string {
	n := recordInt(record, "escalation_count")
	if n <= 0 {
		return ""
	}
	return "#" + strconv.Itoa(n)
}

// recordInt reads a counter off the alert map, tolerating the numeric shapes a
// JSON hop and the two drivers produce.
func recordInt(record jiraadf.RecordSummary, key string) int {
	switch v := record[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
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
func (f *forwarder) createNew(ctx context.Context, env envelope, record jiraadf.RecordSummary, recordHash string) (string, error) {
	projectKey := env.ProjectKey
	if projectKey == "" {
		projectKey = f.cfg.ProjectKey
	}
	if projectKey == "" {
		return "", errors.New("missing project_key")
	}

	issueType, issueTypeID := resolveIssueType(env, f.cfg)
	priorityField, priorityName := f.resolvePriority(ctx, env, record, projectKey, issueTypeID)
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

	summary := f.formatSummary(record, env)
	description := f.formatDescription(record)

	if env.Message != "" {
		description = jiraadf.AppendStrongLine(description, "Custom message", env.Message)
	}
	if env.NotificationName != "" {
		text := "Notified by " + env.NotificationName
		if env.NotificationMsg != "" {
			text += ": " + env.NotificationMsg
		}
		description = jiraadf.AppendPlainLine(description, text)
	}

	req := CreateIssueRequest{
		ProjectKey:    projectKey,
		IssueType:     issueType,
		IssueTypeID:   issueTypeID,
		Summary:       summary,
		Description:   description,
		PriorityField: priorityField,
		Priority:      priorityName,
		Labels:        labels,
		ExtraFields:   extra,
	}
	resp, err := f.jira.CreateIssue(ctx, req)
	if err != nil && priorityRejected(err) {
		// The scheme changed under us (or was never valid): drop the cached
		// copy, resolve again from a fresh fetch, and retry once. If the
		// retry would send the same thing, don't bother.
		f.priorities.Invalidate(priorityCacheKey(projectKey, issueTypeID))
		retryField, retryName := f.resolvePriority(ctx, env, record, projectKey, issueTypeID)
		if !samePriority(priorityField, priorityName, retryField, retryName) {
			f.logger.Info("jira: priority rejected, retrying with a freshly resolved scheme",
				slog.Any("rejected", priorityField),
				slog.Any("retry", retryField))
			req.PriorityField, req.Priority = retryField, retryName
			resp, err = f.jira.CreateIssue(ctx, req)
		}
	}
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

// resolvePriority picks the `fields.priority` value for a new issue.
//
// It returns (field, "") once the live priority scheme is known — always the
// id form, because priority *names* are localized per JIRA site and a
// name-keyed default is wrong on arrival for most of them. The second return
// is the legacy name, used only when scheme discovery failed and we have
// nothing better than the operator's string. Both empty means "omit the
// field" and let JIRA apply the scheme's own default.
func (f *forwarder) resolvePriority(ctx context.Context, env envelope,
	record jiraadf.RecordSummary, projectKey, issueTypeID string) (map[string]any, string) {
	severity := strings.ToLower(strField(record, "severity", ""))
	candidates := priorityCandidates(env, severity, f.cfg)

	key := priorityCacheKey(projectKey, issueTypeID)
	scheme, err := f.priorities.Get(ctx, key, func(ctx context.Context) (jirapriority.Scheme, error) {
		sch, err := f.jira.PriorityScheme(ctx, projectKey, issueTypeID)
		if err == nil {
			f.logger.Info("jira: resolved priority scheme",
				slog.String("project", projectKey),
				slog.String("issue_type_id", issueTypeID),
				slog.Any("priorities", schemeNames(sch)))
		}
		return sch, err
	})
	if err != nil {
		// No scheme: fall back to the pre-resolver behaviour so a site we
		// can't introspect keeps working exactly as it did before.
		legacy := firstNonEmpty(candidates)
		f.logger.Warn("jira: priority scheme lookup failed, sending the configured priority name",
			slog.String("project", projectKey),
			slog.String("priority", legacy),
			slog.Any("err", err))
		return nil, legacy
	}

	// An operator override wins when it actually names something in the
	// scheme — by id first, then by name.
	for _, candidate := range candidates {
		if res := scheme.Resolve(candidate, ""); res.Priority.ID != "" {
			return res.Field(), ""
		}
	}
	// Otherwise place the severity positionally in the scheme.
	if res := scheme.Resolve("", severity); res.Priority.ID != "" {
		f.logger.Debug("jira: priority resolved from severity",
			slog.String("severity", severity),
			slog.String("priority", res.Priority.Name),
			slog.String("priority_id", res.Priority.ID))
		return res.Field(), ""
	}
	f.logger.Debug("jira: no priority resolved, letting JIRA apply its default",
		slog.String("severity", severity))
	return nil, ""
}

// priorityCandidates lists the operator-supplied priority values to try, in
// precedence order: payload override, severity mapping, config fallback.
func priorityCandidates(env envelope, severity string, cfg Config) []string {
	out := make([]string, 0, 3)
	if env.Priority != "" {
		out = append(out, env.Priority)
	}
	if mapped, ok := cfg.PriorityMapping[severity]; ok && mapped != "" {
		out = append(out, mapped)
	}
	if cfg.Priority != "" {
		out = append(out, cfg.Priority)
	}
	return out
}

// priorityCacheKey scopes a cached scheme to the project and issue type it was
// read for — the create screen can expose different priorities per issue type.
func priorityCacheKey(projectKey, issueTypeID string) string {
	return projectKey + "/" + issueTypeID
}

// schemeNames renders a scheme for logging: "1=Critique, 2=Grave, …".
func schemeNames(sch jirapriority.Scheme) []string {
	out := make([]string, 0, len(sch))
	for _, p := range sch {
		out = append(out, p.ID+"="+p.Name)
	}
	return out
}

// firstNonEmpty returns the first non-empty entry, or "".
func firstNonEmpty(values []string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// samePriority reports whether two resolutions would put the same thing on the
// wire, so a pointless retry can be skipped.
func samePriority(fieldA map[string]any, nameA string, fieldB map[string]any, nameB string) bool {
	if nameA != nameB {
		return false
	}
	if len(fieldA) != len(fieldB) {
		return false
	}
	for k, v := range fieldA {
		if fieldB[k] != v {
			return false
		}
	}
	return true
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
func templateVars(record jiraadf.RecordSummary, snoozeURL string) *strings.Replacer {
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

// formatSummary renders the issue title against the record and clamps the
// result to 255 characters (JIRA's summary field limit). The template is
// picked with the payload-over-config precedence used everywhere else:
// envelope `summary` > envelope `summary_template` > cfg.SummaryTemplate.
// All three go through the same ${var} expansion, so a per-alert override can
// be either a literal title or a template.
func (f *forwarder) formatSummary(record jiraadf.RecordSummary, env envelope) string {
	tmpl := chooseString(strings.TrimSpace(env.Summary), strings.TrimSpace(env.SummaryTemplate))
	tmpl = chooseString(tmpl, f.cfg.SummaryTemplate)
	rendered := templateVars(record, f.cfg.SnoozeURL).Replace(tmpl)
	if len(rendered) > 255 {
		rendered = rendered[:255]
	}
	return rendered
}

// formatDescription renders cfg.DescriptionTemplate when set, or falls back
// to the canonical rich ADF description.
func (f *forwarder) formatDescription(record jiraadf.RecordSummary) jiraadf.ADF {
	if f.cfg.DescriptionTemplate == "" {
		return jiraadf.BuildDescriptionADF(record, f.cfg.SnoozeURL)
	}
	rendered := templateVars(record, f.cfg.SnoozeURL).Replace(f.cfg.DescriptionTemplate)
	return jiraadf.TextADF(rendered)
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

// strField returns rec[key] as a trimmed string, or fallback when the key is
// missing or not a string.
func strField(rec jiraadf.RecordSummary, key, fallback string) string {
	v, ok := rec[key]
	if !ok || v == nil {
		return fallback
	}
	if s, ok := v.(string); ok {
		if t := strings.TrimSpace(s); t != "" {
			return t
		}
	}
	return fallback
}
