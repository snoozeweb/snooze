package teams

import (
	"context"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/japannext/snooze/pkg/snoozeclient"
	"github.com/japannext/snooze/pkg/snoozetypes"
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

// Run drives the polling loop until ctx is cancelled. It performs an eager
// Snooze login (best-effort: a login failure is logged but does not abort —
// the lazy /api/v1/alerts path will retry on demand) then starts polling
// Graph for new channel messages every PollInterval.
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
		slog.Duration("poll_interval", d.cfg.PollInterval))

	ticker := time.NewTicker(d.cfg.PollInterval)
	defer ticker.Stop()

	// Drive one immediate poll before entering the tick loop so the daemon
	// is responsive on boot rather than waiting a full interval.
	if err := d.pollOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
		d.logger.Warn("teams: initial poll failed", slog.Any("err", err))
	}

	for {
		select {
		case <-ctx.Done():
			d.logger.Info("teams: daemon stopping", slog.Any("cause", ctx.Err()))
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

// SendAlert formats record as an HTML chatMessage and POSTs it to the
// configured channel. It is exported so other components (e.g. snooze-server
// notification routes hitting an in-process bridge) can reuse it; the polling
// loop does not call this directly.
func (d *Daemon) SendAlert(ctx context.Context, rec snoozetypes.Record) error {
	body := formatAlertHTML(rec)
	if _, err := d.graph.sendMessage(ctx, d.cfg.TeamID, d.cfg.ChannelID, body); err != nil {
		return fmt.Errorf("teams: post alert: %w", err)
	}
	return nil
}

// formatAlertHTML renders a snooze Record as a minimal HTML message. It
// emits the bot marker first so the poll loop's self-detection works.
func formatAlertHTML(rec snoozetypes.Record) string {
	var b []byte
	b = append(b, botMarker...)
	b = append(b, "<b>New alert</b><br>"...)
	if rec.Host != "" {
		b = append(b, "Host: <code>"...)
		b = append(b, html.EscapeString(rec.Host)...)
		b = append(b, "</code><br>"...)
	}
	if rec.Source != "" {
		b = append(b, "Source: <code>"...)
		b = append(b, html.EscapeString(rec.Source)...)
		b = append(b, "</code><br>"...)
	}
	if rec.Severity != "" {
		b = append(b, "Severity: <code>"...)
		b = append(b, html.EscapeString(rec.Severity)...)
		b = append(b, "</code><br>"...)
	}
	if rec.Message != "" {
		b = append(b, html.EscapeString(rec.Message)...)
	}
	return string(b)
}
