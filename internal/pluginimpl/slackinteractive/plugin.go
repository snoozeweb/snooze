// Package slackinteractive implements the "slackinteractive" WebhookReceiver
// plugin (Plan 36). It receives Slack interactivity payloads fired when an
// operator presses an ack/close/re-open button under an alert message (rendered
// by the slack notifier's interactive mode), verifies the Slack request
// signature, applies the state transition through the shared chataction helper
// (so the Plan 05 guard + comment side-effects fire), and returns a
// message-replacement JSON body so Slack edits the message in place.
//
// # Authenticity (fail closed)
//
// Every request is authenticated by the Slack v0 request signature:
//
//	X-Slack-Signature = "v0=" + hex(HMAC-SHA256(signing_secret,
//	                                "v0:" + X-Slack-Request-Timestamp + ":" + rawBody))
//
// compared in constant time. Requests whose timestamp is more than 5 minutes
// from now are rejected (replay defense). An unset signing_secret (the default)
// makes every request 401 — a public, unauthenticated ack/close endpoint is
// unacceptable.
package slackinteractive

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/snoozeweb/snooze/internal/pluginimpl/chataction"
	"github.com/snoozeweb/snooze/internal/pluginimpl/comment"
	"github.com/snoozeweb/snooze/internal/plugins"
)

//go:embed metadata.yaml
var metaYAML []byte

func init() {
	plugins.Register("slackinteractive", metaYAML, factory)
}

const (
	// signatureHeader and timestampHeader are Slack's signing headers.
	signatureHeader = "X-Slack-Signature"
	timestampHeader = "X-Slack-Request-Timestamp"
	// maxSkew is the maximum allowed difference between the request timestamp
	// and now (Slack's recommended replay window).
	maxSkew = 5 * time.Minute
)

// factory is the plugins.Factory entry-point.
func factory(meta plugins.Metadata) (plugins.Plugin, error) {
	return &Plugin{meta: meta}, nil
}

// Plugin is the Slack interactive-message receiver.
type Plugin struct {
	meta plugins.Metadata
	host plugins.Host

	// clock supplies "now" for the request-timestamp skew check. It is injected
	// (overridable from tests) and defaults to time.Now in PostInit — the same
	// pattern the comment/aggregaterule plugins use, since plugins.Host exposes
	// no clock. This keeps a raw time.Now() out of the core verification path and
	// makes the 5-minute window deterministically testable.
	clock func() time.Time
}

// Compile-time proof the plugin satisfies the WebhookReceiver contract.
var _ plugins.WebhookReceiver = (*Plugin)(nil)

// Name returns the registry key.
func (p *Plugin) Name() string { return p.meta.Name }

// Metadata returns the parsed metadata.yaml.
func (p *Plugin) Metadata() plugins.Metadata { return p.meta }

// PostInit wires the host in and defaults the clock.
func (p *Plugin) PostInit(_ context.Context, host plugins.Host) error {
	p.host = host
	if p.clock == nil {
		p.clock = time.Now
	}
	return nil
}

// Reload is a no-op: the plugin holds no cached state.
func (p *Plugin) Reload(_ context.Context) error { return nil }

// WebhookPath returns the route fragment mounted under /api/v1/webhook/.
func (p *Plugin) WebhookPath() string { return "/slack" }

// slackInteraction is the slice of the Slack block_actions payload the receiver
// needs.
type slackInteraction struct {
	Type string `json:"type"`
	User struct {
		Username string `json:"username"`
		Name     string `json:"name"`
	} `json:"user"`
	Actions []struct {
		Value    string `json:"value"`     // action verb (ack/close/open)
		BlockID  string `json:"block_id"`  // record uid
		ActionID string `json:"action_id"` // snooze_<action>
	} `json:"actions"`
	// CallbackID is the legacy attachments-API location for the record uid,
	// accepted as a fallback when block_id is empty.
	CallbackID string `json:"callback_id"`
}

// HandleWebhook serves an inbound Slack interactivity request.
func (p *Plugin) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read the raw body — the signature is computed over it verbatim, so it
	// must be captured before form parsing.
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "cannot read body", http.StatusBadRequest)
		return
	}

	// 1. Verify the Slack signature + timestamp skew. Fail closed.
	if !p.verifySignature(r, raw) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// 2. Slack posts application/x-www-form-urlencoded with a `payload` field
	//    holding the JSON interaction.
	form, err := url.ParseQuery(string(raw))
	if err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	payloadJSON := form.Get("payload")
	if payloadJSON == "" {
		http.Error(w, "missing payload", http.StatusBadRequest)
		return
	}
	var interaction slackInteraction
	if err := json.Unmarshal([]byte(payloadJSON), &interaction); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	if len(interaction.Actions) == 0 {
		// Nothing actionable — ack with an empty 200.
		w.WriteHeader(http.StatusOK)
		return
	}

	action := interaction.Actions[0].Value
	uid := interaction.Actions[0].BlockID
	if uid == "" {
		uid = interaction.CallbackID
	}
	actor := interaction.User.Username
	if actor == "" {
		actor = interaction.User.Name
	}
	if actor == "" {
		actor = "slack-user"
	}

	// 3. Apply the transition. On an illegal move reply 200 with a refusal
	//    message; Slack treats a non-200 as a user-visible failure, so the chat
	//    UX path always returns 200.
	var respBody []byte
	newState, applyErr := chataction.Apply(r.Context(), p.host, uid, action, actor, "slack")
	switch {
	case applyErr == nil:
		respBody = replacementMessage(fmt.Sprintf("✅ %s by %s", stateVerb(newState), actor))
	case errors.Is(applyErr, comment.ErrInvalidTransition):
		respBody = replacementMessage(fmt.Sprintf("⚠️ Cannot %s: not a valid transition for this alert.", action))
	default:
		if lg := p.logger(); lg != nil {
			lg.Warn("slackinteractive: apply failed", "plugin", p.Name(), "uid", uid, "action", action, "err", applyErr)
		}
		respBody = replacementMessage(fmt.Sprintf("⚠️ Could not apply %q.", action))
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(respBody)
}

// verifySignature implements Slack's v0 request-signing scheme with a
// constant-time compare and a 5-minute skew window. It fails closed when the
// signing secret is unset.
func (p *Plugin) verifySignature(r *http.Request, body []byte) bool {
	secret := ""
	if p.host != nil {
		if cfg := p.host.Config(); cfg != nil {
			secret = cfg.SlackInteractive.SigningSecret
		}
	}
	if secret == "" {
		return false // fail closed: no signing secret ⇒ reject everything
	}

	tsHeader := r.Header.Get(timestampHeader)
	if tsHeader == "" {
		return false
	}
	tsSec, err := strconv.ParseInt(tsHeader, 10, 64)
	if err != nil {
		return false
	}
	// Replay window: reject timestamps more than maxSkew from now (either way).
	now := p.now()
	delta := now.Sub(time.Unix(tsSec, 0))
	if delta < 0 {
		delta = -delta
	}
	if delta > maxSkew {
		return false
	}

	got := r.Header.Get(signatureHeader)
	if got == "" {
		return false
	}

	base := "v0:" + tsHeader + ":" + string(body)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(base))
	want := "v0=" + hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(got), []byte(want))
}

// now returns the current time from the injected clock, defaulting to time.Now
// when the plugin was constructed without PostInit (defensive).
func (p *Plugin) now() time.Time {
	if p.clock != nil {
		return p.clock()
	}
	return time.Now()
}

// stateVerb renders a human past-tense for the applied state.
func stateVerb(state string) string {
	switch state {
	case "ack":
		return "Acknowledged"
	case "close":
		return "Closed"
	case "open":
		return "Re-opened"
	case "esc":
		return "Escalated"
	default:
		return state
	}
}

// replacementMessage builds the Slack message-replacement JSON: replace_original
// true, a single mrkdwn section with the status text, and NO actions block (the
// buttons are stripped once the action is taken).
func replacementMessage(text string) []byte {
	resp := map[string]any{
		"replace_original": true,
		"text":             text,
		"blocks": []map[string]any{
			{
				"type": "section",
				"text": map[string]any{"type": "mrkdwn", "text": text},
			},
		},
	}
	b, _ := json.Marshal(resp)
	return b
}

// logger returns the host logger or nil.
func (p *Plugin) logger() interface {
	Warn(string, ...any)
} {
	if p.host == nil {
		return nil
	}
	lg := p.host.Logger()
	if lg == nil {
		return nil
	}
	return lg
}
