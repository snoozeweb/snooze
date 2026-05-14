package jira

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/japannext/snooze/internal/condition"
	"github.com/japannext/snooze/pkg/snoozeclient"
)

// hashURLRegex extracts the alert hash from the value of the configured
// custom field. The Python plugin writes a URL like
// "https://snooze.example.com/web/?#/record?tab=All&s=hash%3Dabc123" and we
// have to round-trip back to the hash before we can close the record.
var hashURLRegex = regexp.MustCompile(`hash(?:%3D|=)([^&\s]+)`)

// snoozeAPI is the narrow surface the poller needs from the Snooze client.
// Tests inject a stub.
type snoozeAPI interface {
	Post(ctx context.Context, path string, body, dest any) error
}

// poller runs the bidirectional status sync: every PollInterval it queries
// JIRA for open tickets carrying the alert-hash custom field; tickets that
// were tracked previously but are no longer in the result set are presumed
// closed, and the corresponding Snooze records are closed too.
type poller struct {
	cfg    Config
	jira   *Client
	snooze snoozeAPI
	logger *slog.Logger

	tracked map[string]string // issueKey → alertHash (last seen open)
}

// newPoller constructs a poller. tracked is initialised empty so the first
// cycle simply seeds the set; closures are only detected from the second
// cycle onward.
func newPoller(cfg Config, jira *Client, sn snoozeAPI, logger *slog.Logger) *poller {
	if logger == nil {
		logger = slog.Default()
	}
	return &poller{
		cfg:     cfg,
		jira:    jira,
		snooze:  sn,
		logger:  logger,
		tracked: map[string]string{},
	}
}

// Run drives the poll loop until ctx is cancelled. Errors inside a cycle are
// logged but do not stop the daemon; a transient JIRA failure shouldn't
// crash the bridge.
func (p *poller) Run(ctx context.Context) {
	p.logger.Info("jira: poller started",
		slog.Duration("interval", p.cfg.PollInterval),
		slog.String("field", p.cfg.AlertHashCustomField))

	ticker := time.NewTicker(p.cfg.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			p.logger.Info("jira: poller stopping")
			return
		case <-ticker.C:
			if err := p.cycle(ctx); err != nil && !errors.Is(err, context.Canceled) {
				p.logger.Warn("jira: poll cycle failed", slog.Any("err", err))
			}
		}
	}
}

// cycle runs one poll iteration: search → diff → close.
func (p *poller) cycle(ctx context.Context) error {
	jql := p.cfg.PollJQL
	if jql == "" {
		jql = defaultJQL(p.cfg.AlertHashCustomField)
	}
	issues, err := p.jira.Search(ctx, jql,
		[]string{p.cfg.AlertHashCustomField, "status"},
		p.cfg.PollMaxResults)
	if err != nil {
		return fmt.Errorf("jira poll: search: %w", err)
	}

	current := map[string]string{}
	for _, iss := range issues {
		raw, ok := iss.Fields[p.cfg.AlertHashCustomField]
		if !ok {
			continue
		}
		value, ok := raw.(string)
		if !ok {
			continue
		}
		hash := extractHash(value)
		if iss.Key != "" && hash != "" {
			current[iss.Key] = hash
		}
	}
	p.logger.Debug("jira: poll cycle",
		slog.Int("open_issues", len(current)),
		slog.Int("previously_tracked", len(p.tracked)))

	for issueKey, hash := range p.tracked {
		if _, stillOpen := current[issueKey]; stillOpen {
			continue
		}
		p.logger.Info("jira: ticket closed in JIRA, closing snooze record",
			slog.String("issue_key", issueKey),
			slog.String("hash", hash))
		if err := p.closeSnoozeRecord(ctx, hash, issueKey); err != nil {
			p.logger.Warn("jira: close snooze record failed",
				slog.String("issue_key", issueKey),
				slog.String("hash", hash),
				slog.Any("err", err))
		}
	}
	p.tracked = current
	return nil
}

// defaultJQL is the canonical JQL used when the operator did not override
// PollJQL. Mirrors the Python plugin so behaviour is identical out of the
// box.
func defaultJQL(field string) string {
	field = strings.TrimSpace(field)
	if strings.HasPrefix(field, "customfield_") {
		num := strings.TrimPrefix(field, "customfield_")
		return fmt.Sprintf("cf[%s] is not EMPTY AND statusCategory != Done", num)
	}
	return fmt.Sprintf("%q is not EMPTY AND statusCategory != Done", field)
}

// extractHash pulls the alert hash out of a custom-field value. The value
// can be either a Snooze URL or a bare hash; in the latter case we return
// it verbatim so a future "store plain hash" config is forward-compatible.
func extractHash(value string) string {
	if m := hashURLRegex.FindStringSubmatch(value); len(m) == 2 {
		decoded, err := url.QueryUnescape(m[1])
		if err == nil {
			return decoded
		}
		return m[1]
	}
	return value
}

// closeSnoozePayload mirrors POST /api/v1/comments shape used to apply a
// "close" action to a record.
type closeSnoozePayload struct {
	Type      string `json:"type"`
	RecordUID string `json:"record_uid"`
	Name      string `json:"name"`
	Method    string `json:"method"`
	Message   string `json:"message"`
}

// recordSearchEnvelope is the response shape of POST /api/v1/record/search.
type recordSearchEnvelope struct {
	Data []struct {
		UID string `json:"uid"`
	} `json:"data"`
}

// closeSnoozeRecord searches for the Snooze record matching alertHash and
// applies a close-comment. It is a no-op when the record has been GC'd or
// was never created.
func (p *poller) closeSnoozeRecord(ctx context.Context, alertHash, issueKey string) error {
	if p.snooze == nil {
		return errors.New("snooze client not configured")
	}
	cond := condition.Equals("hash", alertHash)
	body := map[string]any{"condition": cond}
	var env recordSearchEnvelope
	if err := p.snooze.Post(ctx, "/api/v1/record/search", body, &env); err != nil {
		return fmt.Errorf("search record: %w", err)
	}
	if len(env.Data) == 0 {
		p.logger.Info("jira: no snooze record found for hash",
			slog.String("hash", alertHash), slog.String("issue_key", issueKey))
		return nil
	}
	payloads := make([]closeSnoozePayload, 0, len(env.Data))
	for _, hit := range env.Data {
		if hit.UID == "" {
			continue
		}
		payloads = append(payloads, closeSnoozePayload{
			Type:      "close",
			RecordUID: hit.UID,
			Name:      "jira",
			Method:    "jira",
			Message:   fmt.Sprintf("Closed: JIRA ticket %s resolved", issueKey),
		})
	}
	if len(payloads) == 0 {
		return nil
	}
	return p.snooze.Post(ctx, "/api/v1/comments", payloads, nil)
}

// snoozeClientAdapter unwraps the concrete snoozeclient.Client into the
// snoozeAPI interface used by the poller. It exists so the production wiring
// in daemon.go does not have to know about the interface boundary.
type snoozeClientAdapter struct{ c *snoozeclient.Client }

// Post forwards to the underlying client.
func (a snoozeClientAdapter) Post(ctx context.Context, path string, body, dest any) error {
	return a.c.Post(ctx, path, body, dest)
}
