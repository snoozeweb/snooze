package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/snoozeweb/snooze/internal/jirapriority"
)

// priorityField resolves the `fields.priority` value for a new issue.
//
// Priority names are localized per JIRA site ("Grave" vs "High") and any admin
// can rename them, so the notifier discovers the project's live scheme and
// sends the priority *id* at the position matching the record's severity. The
// action's Priority field is honoured when it names something in that scheme —
// by id first, then by name — and ignored when it doesn't, rather than failing
// the create.
//
// Returns (nil, "") to omit the field entirely and let JIRA apply the scheme
// default. The second return is a legacy priority name, used only when the
// scheme cannot be discovered at all.
func (p *Plugin) priorityField(ctx context.Context, cfg config, severity string) (map[string]any, string) {
	key := cfg.JiraURL + "|" + cfg.ProjectKey + "/" + cfg.IssueType
	scheme, err := p.priorities.Get(ctx, key, func(ctx context.Context) (jirapriority.Scheme, error) {
		return p.fetchScheme(ctx, cfg)
	})
	if err != nil {
		p.logf("jira: priority scheme lookup failed, sending the configured priority name",
			"error", err, "priority", cfg.Priority)
		return nil, cfg.Priority
	}
	if res := scheme.Resolve(cfg.Priority, severity); res.Priority.ID != "" {
		return res.Field(), ""
	}
	return nil, ""
}

// fetchScheme reads the project's priority scheme from createmeta, falling
// back to the site-wide priority list when the field is not on the create
// screen.
func (p *Plugin) fetchScheme(ctx context.Context, cfg config) (jirapriority.Scheme, error) {
	q := url.Values{}
	q.Set("projectKeys", cfg.ProjectKey)
	q.Set("expand", "projects.issuetypes.fields")
	raw, err := p.getJSON(ctx, cfg, cfg.JiraURL+"/rest/api/3/issue/createmeta?"+q.Encode())
	if err == nil {
		if sch, perr := jirapriority.ParseCreateMeta(raw, ""); perr == nil && len(sch) > 0 {
			return sch, nil
		}
	} else {
		p.logf("jira: createmeta lookup failed, falling back to the global priority list", "error", err)
	}
	raw, err = p.getJSON(ctx, cfg, cfg.JiraURL+"/rest/api/3/priority")
	if err != nil {
		return nil, err
	}
	return jirapriority.ParsePriorityList(raw)
}

// getJSON performs an authenticated GET and returns the body.
func (p *Plugin) getJSON(ctx context.Context, cfg config, rawURL string) ([]byte, error) {
	req, err := p.newRequest(ctx, cfg, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := p.newClient(cfg.Timeout).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxSchemeBytes))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("jira: GET %s: HTTP %d: %s", rawURL, resp.StatusCode, truncate(body, 200))
	}
	return body, nil
}

// maxSchemeBytes caps a scheme-discovery response. createmeta for one project
// is a few KiB; 1 MiB is a generous ceiling that still bounds memory.
const maxSchemeBytes = 1 << 20

// priorityRejected reports whether a create response is a 400 blaming the
// `priority` field. Only the field name is inspected — the message text is
// localized by the JIRA site.
func priorityRejected(status int, body []byte) bool {
	if status != http.StatusBadRequest {
		return false
	}
	var env struct {
		Errors map[string]string `json:"errors"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return false
	}
	return strings.TrimSpace(env.Errors["priority"]) != ""
}

// logf logs through the host when one is attached. The notifier runs inside
// snooze-server, so a nil host (tests, bare construction) must stay silent
// rather than panic.
func (p *Plugin) logf(msg string, args ...any) {
	if p.host == nil {
		return
	}
	if lg := p.host.Logger(); lg != nil {
		lg.Warn(msg, args...)
	}
}
