package teams

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// listenerShutdownGrace is how long the HTTP server is given to drain
// in-flight /alert requests after ctx cancels before its connections are
// force-closed.
const listenerShutdownGrace = 5 * time.Second

// alertRequest models the JSON body the legacy Python snooze-teams accepted
// on POST /alert. The webhook plugin used to render this shape from each
// notification action's `payload` template; we keep the same wire contract so
// existing action records ported from 1.x dispatch without operator change.
//
// One channel string takes the form `teams/{teamID}/channels/{channelID}`
// (the prefix is required so the same listener could one day serve
// non-Teams chat backends without ambiguity).
type alertRequest struct {
	Channels     []string           `json:"channels"`
	Alert        snoozetypes.Record `json:"alert"`
	Message      string             `json:"message,omitempty"`
	MessageGroup string             `json:"message_group,omitempty"`
	// Reply is kept for wire-compat with operators who supply a free-form
	// reply text; the bridge does not act on it today.
	Reply string `json:"reply,omitempty"`
	// ReplyToIDs maps a channel ref (`teams/<teamID>/channels/<channelID>`)
	// to the Graph message id this alert should be posted as a reply under.
	// Populated by the webhook payload template after a previous send
	// landed and the webhook plugin captured the bridge's response via
	// `inject_response`. When unset for a given channel, the message lands
	// as a new top-level thread.
	ReplyToIDs map[string]string `json:"reply_to_ids,omitempty"`
}

// alertResponse is the per-channel delivery outcome surfaced back to the
// caller for diagnostics. When the webhook plugin's `inject_response` is on,
// MessageIDs is what gets stamped onto the record so the *next* firing of
// the same alert can supply ReplyToIDs and chain the conversation.
type alertResponse struct {
	Delivered []string          `json:"delivered"`
	Failed    map[string]string `json:"failed,omitempty"`
	// MessageIDs maps a channel ref to the THREAD ROOT message id for that
	// channel. On a fresh send it is the id Graph assigned the new top-level
	// message; on a reply it is the root the reply landed under (the id from
	// the request's reply_to_ids), NOT the reply's own id. Keeping the root
	// stable lets inject_response stamp it back so every subsequent firing
	// threads under the same root — Graph only supports one level of replies.
	MessageIDs map[string]string `json:"message_ids,omitempty"`
}

// runListener starts the HTTP receiver and blocks until ctx is cancelled. It
// returns nil on a clean shutdown; non-context errors propagate up so the
// supervisor can restart the daemon.
func (d *Daemon) runListener(ctx context.Context) error {
	if d.cfg.ListenAddr == "" {
		// Listener disabled — older deployments that only consume the
		// inbound poller keep working unchanged.
		return nil
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/alert", d.handleAlert)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	srv := &http.Server{
		Addr:              d.cfg.ListenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}

	d.logger.Info("teams: listener starting", slog.String("addr", d.cfg.ListenAddr))

	errCh := make(chan error, 1)
	go func() {
		err := srv.ListenAndServe()
		if !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), listenerShutdownGrace)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		<-errCh
		return nil
	case err := <-errCh:
		return fmt.Errorf("teams: listener: %w", err)
	}
}

// handleAlert is the legacy bridge endpoint. It accepts either a single
// alertRequest or an array of them, then posts one Graph message per
// (channel, alert) pair. Failures on a single channel are recorded in the
// response but do not abort the batch.
func (d *Daemon) handleAlert(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	if err != nil {
		http.Error(w, `{"error":"read body"}`, http.StatusBadRequest)
		return
	}
	defer r.Body.Close() //nolint:errcheck

	medias, err := decodeAlertBody(body)
	if err != nil {
		d.logger.Warn("teams: /alert decode failed", slog.Any("err", err))
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}

	// Group alerts by destination channel — when the request contains
	// multiple alerts (the webhook plugin's batched JSON-array form), each
	// channel should see ONE chatMessage covering only the alerts that
	// actually target it. This mirrors the Python plugin's per-channel
	// fan-in while making sure a channel never sees alerts meant for a
	// different team/channel.
	//
	// replyTo carries the per-channel reply pointer. When ANY incoming
	// alertRequest in this batch named a ReplyToIDs[ch] entry, that id is
	// used as the thread root for the consolidated chatMessage on ch. The
	// "last writer wins" rule is harmless: in the common case (one alert,
	// one channel, one pointer) there's only one value.
	type chanGroup struct {
		teamID, channelID, ref string
		alerts                 []snoozetypes.Record
	}
	groups := make(map[string]*chanGroup)
	order := make([]string, 0)
	replyTo := make(map[string]string)
	resp := alertResponse{Failed: map[string]string{}}
	for _, m := range medias {
		for _, ch := range m.Channels {
			teamID, channelID, err := parseChannelRef(ch)
			if err != nil {
				resp.Failed[ch] = err.Error()
				d.logger.Warn("teams: /alert bad channel", slog.String("channel", ch), slog.Any("err", err))
				continue
			}
			g, ok := groups[ch]
			if !ok {
				g = &chanGroup{teamID: teamID, channelID: channelID, ref: ch}
				groups[ch] = g
				order = append(order, ch)
			}
			g.alerts = append(g.alerts, m.Alert)
			if id, ok := m.ReplyToIDs[ch]; ok && id != "" {
				replyTo[ch] = id
			}
		}
	}
	for _, ch := range order {
		g := groups[ch]
		body, att := formatAlertsCard(g.alerts, d.cfg.Server)
		opts := sendOpts{Attachments: []chatAttachment{att}, ReplyToID: replyTo[ch]}
		msg, err := d.graph.sendMessage(r.Context(), g.teamID, g.channelID, body, opts)
		if err != nil {
			resp.Failed[ch] = err.Error()
			d.logger.Warn("teams: /alert post failed",
				slog.String("team", g.teamID),
				slog.String("channel", g.channelID),
				slog.Int("alerts", len(g.alerts)),
				slog.String("reply_to", replyTo[ch]),
				slog.Any("err", err))
			continue
		}
		// Cache (channel, message_id) → record_uid for the chat-handler's
		// thread→record lookup. We populate from every alert in the batch
		// so a Teams reply on any of them resolves to its source record.
		// `g.alerts[0].UID` is the per-record uid the snooze server
		// stamped on the inbound /alert payload; subsequent alerts in
		// the batch share the same thread root because the bridge
		// collapses them into one chatMessage (see the chanGroup logic
		// above).
		if d.threads != nil && msg.ID != "" {
			for _, a := range g.alerts {
				if a.UID == "" {
					continue
				}
				d.threads.Put(ch, msg.ID, a.UID)
			}
		}
		d.logger.Info("teams: /alert delivered",
			slog.String("channel", g.channelID),
			slog.Int("alerts", len(g.alerts)),
			slog.String("reply_to", replyTo[ch]),
			slog.String("message_id", msg.ID))
		resp.Delivered = append(resp.Delivered, ch)
		// Record the THREAD ROOT id for this channel so the next firing chains
		// onto the same thread. When we just posted a reply, the root is the id
		// we replied under (replyTo[ch]) — NOT msg.ID, which is the reply's own
		// id. MS Graph only allows replies one level deep, so the next
		// follow-up must target the original root again; recording the reply id
		// would make it POST /messages/<reply>/replies, which Graph rejects.
		// When we posted a fresh top-level message, msg.ID *is* the root.
		rootID := msg.ID
		if replyTo[ch] != "" {
			rootID = replyTo[ch]
		}
		if rootID != "" {
			if resp.MessageIDs == nil {
				resp.MessageIDs = make(map[string]string, len(order))
			}
			resp.MessageIDs[ch] = rootID
		}
	}

	w.Header().Set("Content-Type", "application/json")
	status := http.StatusOK
	if len(resp.Failed) > 0 && len(resp.Delivered) == 0 {
		status = http.StatusBadGateway
	}
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		d.logger.Warn("teams: /alert response encode", slog.Any("err", err))
	}
}

// decodeAlertBody parses a body that is either a single alertRequest object
// or a JSON array of them. The Python webhook plugin batched multiple records
// into the array form; the non-batched path produces a bare object.
func decodeAlertBody(body []byte) ([]alertRequest, error) {
	trimmed := bytes_trimSpace(body)
	if len(trimmed) == 0 {
		return nil, errors.New("empty body")
	}
	switch trimmed[0] {
	case '[':
		var arr []alertRequest
		if err := json.Unmarshal(body, &arr); err != nil {
			return nil, fmt.Errorf("decode array: %w", err)
		}
		return arr, nil
	case '{':
		var one alertRequest
		if err := json.Unmarshal(body, &one); err != nil {
			return nil, fmt.Errorf("decode object: %w", err)
		}
		return []alertRequest{one}, nil
	default:
		return nil, errors.New("body must be a JSON object or array")
	}
}

// bytes_trimSpace is a tiny stdlib-free trim used by decodeAlertBody so we
// don't pull `bytes` into the package just for one call site.
func bytes_trimSpace(b []byte) []byte {
	start := 0
	for start < len(b) && (b[start] == ' ' || b[start] == '\t' || b[start] == '\r' || b[start] == '\n') {
		start++
	}
	end := len(b)
	for end > start && (b[end-1] == ' ' || b[end-1] == '\t' || b[end-1] == '\r' || b[end-1] == '\n') {
		end--
	}
	return b[start:end]
}

// parseChannelRef splits the `teams/<teamID>/channels/<channelID>` token
// the webhook templates send. The channelID often contains a `:` (the Graph
// `19:xxx@thread.tacv2` form) but never a `/`, so the split is straightforward.
func parseChannelRef(ref string) (teamID, channelID string, err error) {
	parts := strings.SplitN(ref, "/", 4)
	if len(parts) != 4 || parts[0] != "teams" || parts[2] != "channels" {
		return "", "", fmt.Errorf("channel %q must look like teams/<teamID>/channels/<channelID>", ref)
	}
	if parts[1] == "" || parts[3] == "" {
		return "", "", fmt.Errorf("channel %q has empty teamID or channelID", ref)
	}
	return parts[1], parts[3], nil
}

