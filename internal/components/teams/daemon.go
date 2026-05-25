package teams

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/snoozeweb/snooze/pkg/snoozeclient"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// Daemon is the snooze-teams entry point. It owns:
//
//   - A Snooze REST client used both for inbound (Teams → snooze) and for
//     pulling the next batch of alerts to post.
//   - A Microsoft Graph client used for outbound posting and message polling.
//   - A polling loop that turns @mentions into Snooze actions.
//
// Run is the canonical entry point. It blocks until ctx is cancelled and
// returns the first fatal error from any subsystem.
type Daemon struct {
	cfg    Config
	logger *slog.Logger

	graph     *graphClient
	snooze    *snoozeclient.Client
	forwarder *forwarder

	// seenMu guards seen — the polling loop's de-dup memory. We bound the
	// set with seenMax LRU-style to avoid unbounded growth on busy channels.
	seenMu  sync.Mutex
	seen    map[string]time.Time
	seenMax int

	// since is the high-water-mark for createdDateTime; messages older than
	// since on a given poll are assumed to have been processed in a previous
	// cycle (or before the daemon started, modulo Config.PollLookback).
	since time.Time
}

// New constructs a Daemon from a validated Config. It does no network I/O —
// callers should call Run to actually drive the bridge.
func New(cfg Config, logger *slog.Logger) (*Daemon, error) {
	cfg, err := cfg.WithDefaults()
	if err != nil {
		return nil, err
	}
	if logger == nil {
		logger = slog.Default()
	}
	httpc := &http.Client{Timeout: cfg.RequestTimeout}

	sc, err := snoozeclient.New(snoozeclient.Options{
		BaseURL:  cfg.Server,
		Username: cfg.Username,
		Password: cfg.Password,
		Method:   cfg.Method,
		Token:    cfg.Token,
		Insecure: cfg.Insecure,
		Logger:   logger,
		Timeout:  cfg.RequestTimeout,
	})
	if err != nil {
		return nil, fmt.Errorf("teams: build snooze client: %w", err)
	}

	d := &Daemon{
		cfg:     cfg,
		logger:  logger,
		graph:   newGraphClient(cfg, httpc),
		snooze:  sc,
		seen:    make(map[string]time.Time),
		seenMax: 2048,
		since:   time.Now().Add(-cfg.PollLookback),
	}
	d.forwarder = newForwarder(sc, cfg.TeamID, cfg.ChannelID)
	return d, nil
}

// Run drives the polling loop and the legacy /alert HTTP listener until ctx
// is cancelled. It performs an eager Snooze login (best-effort: a login
// failure is logged but does not abort — the lazy /api/v1/alerts path will
// retry on demand) then runs both subsystems under a single errgroup so a
// failure in either tears the daemon down for the supervisor to restart.
//
// Token acquisition for Graph is lazy: the first sendMessage / fetchMessages
// triggers fetchToken.
func (d *Daemon) Run(ctx context.Context) error {
	if d.cfg.Token == "" && d.cfg.Username != "" {
		if err := d.snooze.Login(ctx); err != nil {
			d.logger.Warn("teams: snooze login failed; will retry lazily", slog.Any("err", err))
		}
	}

	d.logger.Info("teams: daemon starting",
		slog.String("graph_base", d.cfg.GraphBase),
		slog.String("team_id", d.cfg.TeamID),
		slog.String("channel_id", d.cfg.ChannelID),
		slog.Duration("poll_interval", d.cfg.PollInterval),
		slog.String("listen_addr", d.cfg.ListenAddr))

	pollErrCh := make(chan error, 1)
	listenErrCh := make(chan error, 1)
	subCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() { pollErrCh <- d.runPollLoop(subCtx) }()
	go func() { listenErrCh <- d.runListener(subCtx) }()

	var firstErr error
	for i := 0; i < 2; i++ {
		select {
		case err := <-pollErrCh:
			if err != nil && !errors.Is(err, context.Canceled) && firstErr == nil {
				firstErr = err
			}
			cancel()
		case err := <-listenErrCh:
			if err != nil && !errors.Is(err, context.Canceled) && firstErr == nil {
				firstErr = err
			}
			cancel()
		}
	}
	d.logger.Info("teams: daemon stopping", slog.Any("cause", ctx.Err()))
	return firstErr
}

// runPollLoop drives the inbound Graph poller. Extracted from Run so the new
// errgroup-style supervision in Run stays readable.
func (d *Daemon) runPollLoop(ctx context.Context) error {
	ticker := time.NewTicker(d.cfg.PollInterval)
	defer ticker.Stop()

	if err := d.pollOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
		d.logger.Warn("teams: initial poll failed", slog.Any("err", err))
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := d.pollOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
				d.logger.Warn("teams: poll failed", slog.Any("err", err))
			}
		}
	}
}

// pollOnce fetches the latest batch of channel messages and dispatches each
// new one through parseCommand → forwardCommand. Errors at the message level
// are logged but do not abort the batch — one malformed message must not
// prevent the next from being processed.
func (d *Daemon) pollOnce(ctx context.Context) error {
	msgs, err := d.graph.fetchMessages(ctx, d.cfg.TeamID, d.cfg.ChannelID)
	if err != nil {
		return err
	}
	for _, m := range msgs {
		if m.ID == "" {
			continue
		}
		if d.markSeen(m.ID) {
			continue
		}
		if !m.CreatedDateTime.IsZero() && m.CreatedDateTime.Before(d.since) {
			continue
		}
		cmd, ok := parseCommand(m, "", d.cfg.BotName, d.cfg.BotName)
		if !ok {
			continue
		}
		if cmd.Verb == "" {
			continue
		}
		if _, err := d.forwarder.forwardCommand(ctx, cmd); err != nil {
			d.logger.Warn("teams: forward command failed",
				slog.String("verb", cmd.Verb),
				slog.String("speaker", cmd.Speaker),
				slog.Any("err", err))
			continue
		}
		d.logger.Info("teams: forwarded command",
			slog.String("verb", cmd.Verb),
			slog.String("speaker", cmd.Speaker))
		if !m.CreatedDateTime.IsZero() && m.CreatedDateTime.After(d.since) {
			d.since = m.CreatedDateTime
		}
	}
	return nil
}

// markSeen records id in the de-dup set and returns true when the id was
// already present (i.e. the caller should skip it). The set is capped at
// seenMax entries; when over capacity we evict the oldest half.
func (d *Daemon) markSeen(id string) bool {
	d.seenMu.Lock()
	defer d.seenMu.Unlock()
	if _, ok := d.seen[id]; ok {
		return true
	}
	if len(d.seen) >= d.seenMax {
		// Drop entries older than 1h, preserving recent activity.
		cutoff := time.Now().Add(-time.Hour)
		for k, v := range d.seen {
			if v.Before(cutoff) {
				delete(d.seen, k)
			}
		}
		// If we still haven't pruned anything (e.g. burst of new ids in
		// the same minute), fall back to dropping arbitrary entries until
		// we're back under cap. Map iteration order is randomised so the
		// eviction set is effectively random — good enough for a poller.
		for k := range d.seen {
			if len(d.seen) < d.seenMax {
				break
			}
			delete(d.seen, k)
		}
	}
	d.seen[id] = time.Now()
	return false
}

// SendAlert formats record as an Adaptive Card chatMessage and POSTs it to
// the configured channel. It is exported so other components (e.g.
// snooze-server notification routes hitting an in-process bridge) can reuse
// it; the polling loop does not call this directly.
func (d *Daemon) SendAlert(ctx context.Context, rec snoozetypes.Record) error {
	body, att := formatAlertCard(rec, d.cfg.Server)
	if _, err := d.graph.sendMessage(ctx, d.cfg.TeamID, d.cfg.ChannelID, body, att); err != nil {
		return fmt.Errorf("teams: post alert: %w", err)
	}
	return nil
}

// alertTimestampFormat mirrors the Python plugin's `date_format` default —
// "Mon, May 25, 2026 at 10:30 AM" — so messages render identically to the
// 1.x bot.
const alertTimestampFormat = "Mon, Jan 2, 2006 at 3:04 PM"

// recordHash returns the duplicate-detection hash from the record, with a
// best-effort fallback to the Extra map (the API exposes `hash` there until
// it earns a typed home on snoozetypes.Record).
func recordHash(rec snoozetypes.Record) string {
	if rec.Extra != nil {
		if v, ok := rec.Extra["hash"].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

// alertTimestamp returns the record's timestamp formatted with
// alertTimestampFormat, falling back through Timestamp → DateEpoch → now.
func alertTimestamp(rec snoozetypes.Record) string {
	t := rec.Timestamp
	if t.IsZero() && rec.DateEpoch != 0 {
		t = time.Unix(rec.DateEpoch, 0)
	}
	if t.IsZero() {
		t = time.Now()
	}
	return t.Local().Format(alertTimestampFormat)
}

// recordWebLink returns the hyperlinked host name pointing at the snooze web
// UI's hash-filtered record view, matching the Python plugin's layout. When
// either the snooze base URL or the hash is missing, we emit just the
// (escaped) host so the message still renders cleanly.
func recordWebLink(rec snoozetypes.Record, snoozeURL string) string {
	host := rec.Host
	if host == "" {
		host = "Unknown"
	}
	escHost := html.EscapeString(host)
	hash := recordHash(rec)
	if snoozeURL == "" || hash == "" {
		return escHost
	}
	href := fmt.Sprintf("%s/web/?#/record?tab=All&s=hash%%3D%s",
		strings.TrimRight(snoozeURL, "/"), html.EscapeString(hash))
	return fmt.Sprintf(`<a href="%s">%s</a>`, href, escHost)
}

// formatAlertCard renders rec as an Adaptive Card 1.4 chatMessage payload —
// a Graph message body that points at one attached AdaptiveCard JSON
// document. The card layout mirrors the Python 1.x bot:
//
//   - bold header "⚠️ Received alert ⚠️"
//   - subtle timestamp underneath
//   - FactSet with Host/Source/Process/Severity (auto-aligned columns)
//   - emphasis Container holding the alert message in a larger, bolded
//     TextBlock so it visually anchors the card
//
// Returned htmlBody contains the `<attachment id="..."></attachment>` ref
// the Graph API uses to associate the body with the card, plus the
// snooze-bot marker so the poll loop's self-detection (forward.go) still
// works. The caller passes both to sendMessage.
func formatAlertCard(rec snoozetypes.Record, snoozeURL string) (htmlBody string, attachment chatAttachment) {
	id := newAttachmentID()
	card := buildAlertCard(rec, snoozeURL)
	cardJSON, _ := json.Marshal(card)
	attachment = chatAttachment{
		ID:          id,
		ContentType: "application/vnd.microsoft.card.adaptive",
		Content:     string(cardJSON),
	}
	htmlBody = `<attachment id="` + id + `"></attachment>` + botMarker
	return htmlBody, attachment
}

// formatAlertsCard renders one or more alerts targeting the same channel as
// a single Adaptive Card. With one record it falls back to formatAlertCard;
// with multiple it composes a "Received N alerts" header followed by one
// emphasis Container per alert. Splitting per-channel is the caller's job
// (see listener.handleAlert) — this helper makes no assumption about the
// destination.
func formatAlertsCard(records []snoozetypes.Record, snoozeURL string) (htmlBody string, attachment chatAttachment) {
	if len(records) == 1 {
		return formatAlertCard(records[0], snoozeURL)
	}
	id := newAttachmentID()
	card := buildAlertsCard(records, snoozeURL)
	cardJSON, _ := json.Marshal(card)
	attachment = chatAttachment{
		ID:          id,
		ContentType: "application/vnd.microsoft.card.adaptive",
		Content:     string(cardJSON),
	}
	htmlBody = `<attachment id="` + id + `"></attachment>` + botMarker
	return htmlBody, attachment
}

// buildAlertsCard composes the multi-alert AdaptiveCard body: a "Received N
// alerts" header (with the timestamp of the latest record), then one
// emphasis Container per record. Each container packs the alert into three
// stacked TextBlocks — host link, subtle source · process · severity, and
// the bolded message — so the channel sees the whole batch without losing
// the visual anchor the single-alert card provides.
func buildAlertsCard(records []snoozetypes.Record, snoozeURL string) map[string]any {
	body := []map[string]any{
		{
			"type":   "TextBlock",
			"text":   fmt.Sprintf("⚠️ Received %d alerts ⚠️", len(records)),
			"weight": "Bolder",
			"size":   "Medium",
			"wrap":   true,
		},
		{
			"type":     "TextBlock",
			"text":     alertTimestamp(latestAlert(records)),
			"isSubtle": true,
			"spacing":  "None",
			"wrap":     true,
		},
	}
	for _, r := range records {
		body = append(body, buildAlertContainer(r, snoozeURL))
	}
	return map[string]any{
		"$schema": "http://adaptivecards.io/schemas/adaptive-card.json",
		"type":    "AdaptiveCard",
		"version": "1.4",
		"msteams": map[string]any{"width": "full"},
		"body":    body,
	}
}

// buildAlertContainer renders one record as a stacked, emphasis-tinted
// Container suitable for use inside a multi-alert card. The same shape is
// used for every record so the visual rhythm of the card stays predictable
// regardless of how many alerts arrived in the batch.
func buildAlertContainer(rec snoozetypes.Record, snoozeURL string) map[string]any {
	meta := make([]string, 0, 3)
	if rec.Source != "" {
		meta = append(meta, rec.Source)
	}
	if rec.Process != "" {
		meta = append(meta, rec.Process)
	}
	if rec.Severity != "" {
		meta = append(meta, rec.Severity)
	}
	items := []map[string]any{
		{
			"type":   "TextBlock",
			"text":   recordFactValue(rec, snoozeURL),
			"weight": "Bolder",
			"wrap":   true,
		},
	}
	if len(meta) > 0 {
		items = append(items, map[string]any{
			"type":     "TextBlock",
			"text":     strings.Join(meta, " · "),
			"isSubtle": true,
			"color":    severityColor(rec.Severity),
			"spacing":  "None",
			"wrap":     true,
		})
	}
	if rec.Message != "" {
		items = append(items, map[string]any{
			"type":    "TextBlock",
			"text":    rec.Message,
			"weight":  "Bolder",
			"spacing": "Small",
			"wrap":    true,
		})
	}
	return map[string]any{
		"type":    "Container",
		"style":   "emphasis",
		"spacing": "Medium",
		"items":   items,
	}
}

// latestAlert returns the record with the most recent timestamp from
// records, falling back to records[0]. Used by buildAlertsCard to stamp
// the header with the freshest alert's time.
func latestAlert(records []snoozetypes.Record) snoozetypes.Record {
	if len(records) == 0 {
		return snoozetypes.Record{}
	}
	latest := records[0]
	for _, r := range records[1:] {
		if r.Timestamp.After(latest.Timestamp) {
			latest = r
		} else if r.Timestamp.IsZero() && r.DateEpoch > latest.DateEpoch {
			latest = r
		}
	}
	return latest
}

// buildAlertCard assembles the AdaptiveCard 1.4 body. Kept separate from
// formatAlertCard so tests can assert on the card shape without serialising
// it through chatAttachment.
func buildAlertCard(rec snoozetypes.Record, snoozeURL string) map[string]any {
	body := []map[string]any{
		{
			"type":   "TextBlock",
			"text":   "⚠️ Received alert ⚠️",
			"weight": "Bolder",
			"size":   "Medium",
			"wrap":   true,
		},
		{
			"type":     "TextBlock",
			"text":     alertTimestamp(rec),
			"isSubtle": true,
			"spacing":  "None",
			"wrap":     true,
		},
	}

	facts := []map[string]any{
		{"title": "Host", "value": recordFactValue(rec, snoozeURL)},
	}
	if rec.Source != "" {
		facts = append(facts, map[string]any{"title": "Source", "value": rec.Source})
	}
	if rec.Process != "" {
		facts = append(facts, map[string]any{"title": "Process", "value": rec.Process})
	}
	if rec.Severity != "" {
		facts = append(facts, map[string]any{"title": "Severity", "value": rec.Severity})
	}
	body = append(body, map[string]any{
		"type":  "FactSet",
		"facts": facts,
	})

	if rec.Message != "" {
		body = append(body, map[string]any{
			"type":  "Container",
			"style": "emphasis",
			"items": []map[string]any{
				{
					"type":   "TextBlock",
					"text":   rec.Message,
					"weight": "Bolder",
					"size":   "Medium",
					"color":  severityColor(rec.Severity),
					"wrap":   true,
				},
			},
		})
	}

	return map[string]any{
		"$schema": "http://adaptivecards.io/schemas/adaptive-card.json",
		"type":    "AdaptiveCard",
		"version": "1.4",
		"msteams": map[string]any{"width": "full"},
		"body":    body,
	}
}

// recordFactValue returns the host string used inside the FactSet. When the
// snooze URL + record hash are both known, the host is rendered as a
// Markdown link to the record view; AdaptiveCard FactSet values render
// Markdown inline. Otherwise we return the bare host so it still looks
// clean.
func recordFactValue(rec snoozetypes.Record, snoozeURL string) string {
	host := rec.Host
	if host == "" {
		host = "Unknown"
	}
	hash := recordHash(rec)
	if snoozeURL == "" || hash == "" {
		return host
	}
	return fmt.Sprintf("[%s](%s/web/?#/record?tab=All&s=hash%%3D%s)",
		host,
		strings.TrimRight(snoozeURL, "/"),
		hash)
}

// severityColor maps a snooze severity to the AdaptiveCard TextBlock color
// taxonomy ("Default" / "Accent" / "Good" / "Warning" / "Attention").
// Unknown / empty severities fall through to "Default".
func severityColor(severity string) string {
	switch strings.ToLower(severity) {
	case "critical", "emergency", "alert":
		return "Attention"
	case "warning", "warn":
		return "Warning"
	case "info", "informational", "notice":
		return "Accent"
	case "ok", "success":
		return "Good"
	}
	return "Default"
}

// newAttachmentID returns a hex UUIDv4-shaped identifier suitable for the
// `<attachment id>` placeholder. Match the Python plugin's `uuid.uuid4().hex`.
func newAttachmentID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Fall back to a timestamp-derived id so we never block on a
		// degraded RNG; collisions in the attachment id are bounded to
		// the single chatMessage we're constructing.
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	// Set RFC 4122 v4 bits.
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x", b)
}
