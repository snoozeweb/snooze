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
	Reply        string             `json:"reply,omitempty"`
}

// alertResponse is the per-channel delivery outcome surfaced back to the
// caller for diagnostics.
type alertResponse struct {
	Delivered []string           `json:"delivered"`
	Failed    map[string]string  `json:"failed,omitempty"`
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
	type chanGroup struct {
		teamID, channelID, ref string
		alerts                 []snoozetypes.Record
	}
	groups := make(map[string]*chanGroup)
	order := make([]string, 0)
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
		}
	}
	for _, ch := range order {
		g := groups[ch]
		body, att := formatAlertsCard(g.alerts, d.cfg.Server)
		if _, err := d.graph.sendMessage(r.Context(), g.teamID, g.channelID, body, att); err != nil {
			resp.Failed[ch] = err.Error()
			d.logger.Warn("teams: /alert post failed",
				slog.String("team", g.teamID),
				slog.String("channel", g.channelID),
				slog.Int("alerts", len(g.alerts)),
				slog.Any("err", err))
			continue
		}
		d.logger.Info("teams: /alert delivered",
			slog.String("channel", g.channelID),
			slog.Int("alerts", len(g.alerts)))
		resp.Delivered = append(resp.Delivered, ch)
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

