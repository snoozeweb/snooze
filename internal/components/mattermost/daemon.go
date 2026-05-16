package mattermost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/snoozeweb/snooze/pkg/snoozeclient"
)

// Daemon is the long-lived orchestrator wired together by main.go.
// It owns:
//
//   - a Mattermost REST handle (mmAPI) for posting replies + lookups,
//   - a Snooze REST client (snoozeclient.Client) for forwarding intent,
//   - a WebSocket loop with reconnect/backoff.
//
// Construct a Daemon with NewDaemon then call Run(ctx). Run returns nil
// when the context is cancelled, or the first unrecoverable error.
type Daemon struct {
	cfg    *Config
	logger *slog.Logger

	// mm is the Mattermost REST client. Replaced in tests via WithMattermostAPI.
	mm *mmAPI

	// snooze is the v1 REST client used by Forward. Replaced in tests via
	// WithSnoozeAPI (an interface) so we never need real network calls.
	snooze snoozeAPI

	// dialer is the WebSocket constructor; replaced in tests so we can
	// inject a stub gorilla server.
	dialer wsDialer

	// resolved Mattermost identifiers — populated by handshake().
	botUser    *User
	team       *Team
	channelIDs map[string]string // channel ID → channel name
}

// wsDialer abstracts dialWS for tests. Production callers leave Daemon.dialer
// nil and the default dialWS is used.
type wsDialer func(ctx context.Context, siteURL, token string, insecure bool, logger *slog.Logger) (*wsClient, error)

// Option mutates a Daemon during NewDaemon. Tests use these to inject
// stubbed transports without touching public state.
type Option func(*Daemon)

// WithLogger overrides the daemon's slog handle.
func WithLogger(l *slog.Logger) Option {
	return func(d *Daemon) {
		if l != nil {
			d.logger = l
		}
	}
}

// WithMattermostAPI replaces the daemon's Mattermost REST client.
// Intended for tests that already built an mmAPI bound to an httptest server.
func WithMattermostAPI(api *mmAPI) Option {
	return func(d *Daemon) { d.mm = api }
}

// WithSnoozeAPI replaces the daemon's Snooze adapter.
func WithSnoozeAPI(s snoozeAPI) Option {
	return func(d *Daemon) { d.snooze = s }
}

// WithDialer replaces the WebSocket constructor.
func WithDialer(d wsDialer) Option {
	return func(dm *Daemon) { dm.dialer = d }
}

// NewDaemon validates the config and assembles a ready-to-Run Daemon.
// Network calls are deferred to Run so construction stays cheap and tests
// don't need a live server just to instantiate the type.
func NewDaemon(cfg *Config, opts ...Option) (*Daemon, error) {
	if cfg == nil {
		return nil, errors.New("mattermost: nil config")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	d := &Daemon{
		cfg:        cfg,
		logger:     slog.Default(),
		channelIDs: map[string]string{},
	}
	for _, opt := range opts {
		opt(d)
	}
	// Build the Mattermost REST handle unless a test injected one.
	if d.mm == nil {
		d.mm = newAPI(cfg.MattermostURL, cfg.MattermostToken, cfg.Insecure)
	}
	// Build the Snooze client unless a test injected one.
	if d.snooze == nil {
		sc, err := snoozeclient.New(snoozeclient.Options{
			BaseURL:  cfg.Server,
			Username: cfg.Username,
			Password: cfg.Password,
			Method:   cfg.Method,
			Insecure: cfg.Insecure,
			Logger:   d.logger,
		})
		if err != nil {
			return nil, fmt.Errorf("mattermost: build snooze client: %w", err)
		}
		d.snooze = snoozeClientAdapter{c: sc}
	}
	if d.dialer == nil {
		d.dialer = dialWS
	}
	return d, nil
}

// Run executes the daemon loop until ctx is cancelled or an unrecoverable
// error surfaces. It:
//
//  1. Logs into Snooze (best-effort — re-login is also lazy on 401).
//  2. Resolves the Mattermost team and bot user (handshake).
//  3. Opens the WebSocket and reads events.
//  4. On disconnect, sleeps with exponential backoff (capped) and retries.
func (d *Daemon) Run(ctx context.Context) error {
	// Login to Snooze is best-effort; the snoozeclient.Do helper re-logs in
	// on a 401 so a transient failure here is not fatal.
	if c, ok := d.snooze.(snoozeClientAdapter); ok {
		if err := c.c.Login(ctx); err != nil {
			d.logger.Warn("snooze login failed, will retry lazily on 401", slog.Any("err", err))
		}
	}

	if err := d.handshake(ctx); err != nil {
		return err
	}

	backoff := d.cfg.ReconnectInitialBackoff
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := d.runOnce(ctx)
		if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return ctx.Err()
		}
		d.logger.Warn("mattermost ws loop ended, reconnecting", slog.Any("err", err), slog.Duration("backoff", backoff))
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > d.cfg.ReconnectMaxBackoff {
			backoff = d.cfg.ReconnectMaxBackoff
		}
	}
}

// handshake resolves the bot user + team + channel IDs. Run before any WS
// loop iteration so message handling can map channel IDs back to names.
func (d *Daemon) handshake(ctx context.Context) error {
	me, err := d.mm.Me(ctx)
	if err != nil {
		return fmt.Errorf("mattermost: validate token: %w", err)
	}
	d.botUser = me
	team, err := d.mm.TeamByName(ctx, d.cfg.MattermostTeam)
	if err != nil {
		return fmt.Errorf("mattermost: resolve team %q: %w", d.cfg.MattermostTeam, err)
	}
	d.team = team
	for _, name := range d.cfg.Channels {
		ch, err := d.mm.ChannelByName(ctx, team.ID, name)
		if err != nil {
			return fmt.Errorf("mattermost: resolve channel %q: %w", name, err)
		}
		d.channelIDs[ch.ID] = ch.Name
	}
	d.logger.Info("mattermost handshake ok",
		slog.String("user", me.Username),
		slog.String("team", team.Name),
		slog.Int("channels", len(d.channelIDs)),
	)
	return nil
}

// runOnce opens one WebSocket connection, pumps events until the connection
// closes (or ctx is cancelled), and returns. Callers loop with backoff.
func (d *Daemon) runOnce(ctx context.Context) error {
	ws, err := d.dialer(ctx, d.cfg.MattermostURL, d.cfg.MattermostToken, d.cfg.Insecure, d.logger)
	if err != nil {
		return err
	}
	defer ws.Close() //nolint:errcheck
	d.logger.Info("mattermost ws connected", slog.String("url", d.cfg.MattermostURL))

	// Best-effort ping loop. A failed ping closes the socket, which surfaces
	// as a ReadEvent error and triggers reconnect.
	pingCtx, cancelPing := context.WithCancel(ctx)
	defer cancelPing()
	go d.pingLoop(pingCtx, ws)

	for {
		ev, err := ws.ReadEvent(ctx)
		if err != nil {
			return err
		}
		if ev == nil || ev.Event == "" {
			continue
		}
		if err := d.handleEvent(ctx, ev); err != nil {
			d.logger.Warn("mattermost handle event failed", slog.String("event", ev.Event), slog.Any("err", err))
		}
	}
}

// pingLoop emits a no-op ping every cfg.PingInterval until ctx is cancelled.
func (d *Daemon) pingLoop(ctx context.Context, ws *wsClient) {
	t := time.NewTicker(d.cfg.PingInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := ws.SendPing(); err != nil {
				d.logger.Debug("mattermost ws ping failed", slog.Any("err", err))
				return
			}
		}
	}
}

// handleEvent demultiplexes a Mattermost WS event. We only care about
// `posted` events (new messages) — everything else is logged at debug.
func (d *Daemon) handleEvent(ctx context.Context, ev *wsEvent) error {
	if ev.Event != "posted" {
		d.logger.Debug("ignored mattermost event", slog.String("event", ev.Event))
		return nil
	}
	post, channelName, err := decodePostedEvent(ev)
	if err != nil {
		return err
	}
	// Skip our own messages so the bot doesn't talk to itself.
	if d.botUser != nil && post.UserID == d.botUser.ID {
		return nil
	}
	// Respect the channel allow-list if one is configured.
	if len(d.channelIDs) > 0 {
		if _, ok := d.channelIDs[post.ChannelID]; !ok {
			return nil
		}
	}
	if !isBotInvocation(post.Message, d.cfg.BotName) {
		return nil
	}
	cmd := ParseCommand(stripBotPrefix(post.Message, d.cfg.BotName))
	reply := Forward(ctx, d.snooze, cmd, post.SenderName)
	d.logger.Debug("mattermost reply",
		slog.String("channel", channelName),
		slog.String("user", post.SenderName),
		slog.String("verb", verbName(cmd.Kind)),
	)
	_, err = d.mm.CreatePost(ctx, Post{
		ChannelID: post.ChannelID,
		Message:   reply,
		RootID:    rootID(post),
	})
	return err
}

// postedPayload is the shape Mattermost wraps inside `data.post` for a
// `posted` event. The outer event has post as a JSON-encoded string —
// not an object — so we have to unmarshal it twice.
type postedPayload struct {
	ID         string `json:"id"`
	UserID     string `json:"user_id"`
	ChannelID  string `json:"channel_id"`
	Message    string `json:"message"`
	RootID     string `json:"root_id"`
	SenderName string `json:"-"`
}

// decodePostedEvent extracts a postedPayload + channel name from a `posted`
// event envelope. The Mattermost API stringifies the post payload, hence
// the double-unmarshal.
func decodePostedEvent(ev *wsEvent) (postedPayload, string, error) {
	var out postedPayload
	rawPost, ok := ev.Data["post"]
	if !ok {
		return out, "", errors.New("mattermost: posted event missing data.post")
	}
	// data.post is a JSON-encoded string per Mattermost wire spec.
	var postStr string
	if err := json.Unmarshal(rawPost, &postStr); err != nil {
		// Some Mattermost versions ship the post as an inline object — fall
		// back to direct decoding in that case.
		if err2 := json.Unmarshal(rawPost, &out); err2 != nil {
			return out, "", fmt.Errorf("mattermost: decode posted.post: %w / %w", err, err2)
		}
	} else if err := json.Unmarshal([]byte(postStr), &out); err != nil {
		return out, "", fmt.Errorf("mattermost: decode posted.post string: %w", err)
	}
	// sender_name lives at data.sender_name (string-encoded).
	if raw, ok := ev.Data["sender_name"]; ok {
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			out.SenderName = s
		}
	}
	if out.SenderName == "" {
		out.SenderName = "user"
	}
	var channelName string
	if raw, ok := ev.Data["channel_name"]; ok {
		_ = json.Unmarshal(raw, &channelName)
	}
	return out, channelName, nil
}

// isBotInvocation reports whether msg is directed at the bot. A message is
// considered an invocation when it starts with `@<botName>`, `/snooze` or
// the bare verb `snooze`.
func isBotInvocation(msg, botName string) bool {
	trim := strings.TrimSpace(msg)
	if trim == "" {
		return false
	}
	lower := strings.ToLower(trim)
	if strings.HasPrefix(lower, "@"+strings.ToLower(botName)) {
		return true
	}
	if strings.HasPrefix(lower, "/snooze") {
		return true
	}
	return false
}

// stripBotPrefix removes a leading `@botName` from msg so the residual
// string can be fed directly into ParseCommand.
func stripBotPrefix(msg, botName string) string {
	trim := strings.TrimSpace(msg)
	lower := strings.ToLower(trim)
	prefix := "@" + strings.ToLower(botName)
	if strings.HasPrefix(lower, prefix) {
		return strings.TrimSpace(trim[len(prefix):])
	}
	return trim
}

// rootID returns the thread root to reply into. For top-level messages,
// reply to the message itself; for replies, keep the existing root.
func rootID(p postedPayload) string {
	if p.RootID != "" {
		return p.RootID
	}
	return p.ID
}
