// Package telegraminteractive implements the "telegraminteractive"
// WebhookReceiver plugin (Plan 36). It receives Telegram callback_query updates
// fired when an operator presses an ack/close/re-open inline-keyboard button
// under an alert message (rendered by the telegram notifier's interactive mode),
// applies the state transition through the shared chataction helper (so the Plan
// 05 guard + comment side-effects fire), and edits the chat message in place.
//
// # Authenticity
//
// Telegram has no HMAC. The webhook is gated on a per-plugin secret_token matched
// (constant-time) against the X-Telegram-Bot-Api-Secret-Token header Telegram
// sets when the webhook was registered with setWebhook. This is the
// Telegram-native equivalent of a signature and keeps the state-mutation endpoint
// from being unauthenticated.
//
// Fail-closed: an unset secret_token (the default) makes every request 401 — a
// public, unauthenticated ack/close endpoint is unacceptable.
package telegraminteractive

import (
	"bytes"
	"context"
	"crypto/subtle"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/snoozeweb/snooze/internal/pluginimpl/chataction"
	"github.com/snoozeweb/snooze/internal/pluginimpl/comment"
	"github.com/snoozeweb/snooze/internal/plugins"
)

//go:embed metadata.yaml
var metaYAML []byte

func init() {
	plugins.Register("telegraminteractive", metaYAML, factory)
}

// secretTokenHeader is the header Telegram sets on every webhook delivery when
// the webhook was registered with a secret_token.
const secretTokenHeader = "X-Telegram-Bot-Api-Secret-Token" //nolint:gosec // header name, not a secret

// defaultTimeout bounds the outbound editMessageText / answerCallbackQuery calls.
const defaultTimeout = 10 * time.Second

// factory is the plugins.Factory entry-point.
func factory(meta plugins.Metadata) (plugins.Plugin, error) {
	return &Plugin{meta: meta, newClient: defaultClient}, nil
}

// Plugin is the Telegram interactive callback receiver.
type Plugin struct {
	meta plugins.Metadata
	host plugins.Host

	// newClient builds the http.Client for the outbound Telegram API calls.
	// Tests override it to point at an httptest server.
	newClient func(timeout time.Duration) *http.Client
}

// Compile-time proof the plugin satisfies the WebhookReceiver contract.
var _ plugins.WebhookReceiver = (*Plugin)(nil)

// Name returns the registry key.
func (p *Plugin) Name() string { return p.meta.Name }

// Metadata returns the parsed metadata.yaml.
func (p *Plugin) Metadata() plugins.Metadata { return p.meta }

// PostInit wires the host in.
func (p *Plugin) PostInit(_ context.Context, host plugins.Host) error {
	p.host = host
	if p.newClient == nil {
		p.newClient = defaultClient
	}
	return nil
}

// Reload is a no-op: the plugin holds no cached state.
func (p *Plugin) Reload(_ context.Context) error { return nil }

// WebhookPath returns the route fragment mounted under /api/v1/webhook/.
func (p *Plugin) WebhookPath() string { return "/telegram" }

// telegramUpdate is the slice of the Telegram update payload the receiver needs.
type telegramUpdate struct {
	CallbackQuery *struct {
		ID   string `json:"id"`
		Data string `json:"data"`
		From struct {
			Username  string `json:"username"`
			FirstName string `json:"first_name"`
		} `json:"from"`
		Message *struct {
			MessageID int64 `json:"message_id"`
			Chat      struct {
				ID int64 `json:"id"`
			} `json:"chat"`
		} `json:"message"`
	} `json:"callback_query"`
}

// HandleWebhook serves an inbound Telegram callback_query update.
func (p *Plugin) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 1. Authenticity: constant-time compare the secret token. Fail closed on
	//    an unset configured secret.
	if !p.verifySecret(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var update telegramUpdate
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		http.Error(w, fmt.Sprintf("invalid telegram update: %v", err), http.StatusBadRequest)
		return
	}
	cq := update.CallbackQuery
	if cq == nil || cq.Message == nil {
		// Not a button press (e.g. a message update) — nothing to do, ack 200.
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
		return
	}

	action, uid := parseCallbackData(cq.Data)
	actor := cq.From.Username
	if actor == "" {
		actor = cq.From.FirstName
	}
	if actor == "" {
		actor = "telegram-user"
	}

	chatID := cq.Message.Chat.ID
	messageID := cq.Message.MessageID
	ctx := r.Context()

	var replyText string
	newState, err := chataction.Apply(ctx, p.host, uid, action, actor, "telegram")
	switch {
	case err == nil:
		replyText = fmt.Sprintf("✅ %s by %s", stateVerb(newState), actor)
	case errors.Is(err, comment.ErrInvalidTransition):
		// Illegal move: refuse with a user-facing message, leave state unchanged.
		replyText = fmt.Sprintf("⚠️ Cannot %s: not a valid transition for this alert.", action)
	default:
		// Unknown record / DB error: log once and inform the operator. State is
		// unchanged. Still answer 200 for chat-client UX.
		if lg := p.logger(); lg != nil {
			lg.Warn("telegraminteractive: apply failed", "plugin", p.Name(), "uid", uid, "action", action, "err", err)
		}
		replyText = fmt.Sprintf("⚠️ Could not apply %q.", action)
	}

	// 2. Edit the message in place (best-effort) and always answer the callback
	//    so the client spinner clears. The DB is the source of truth; a stale
	//    button is only cosmetic.
	p.editMessageText(ctx, chatID, messageID, replyText)
	p.answerCallbackQuery(ctx, cq.ID, replyText)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

// verifySecret returns true only when the configured secret_token is non-empty
// AND matches the request header in constant time. An unset secret fails closed.
func (p *Plugin) verifySecret(r *http.Request) bool {
	secret := ""
	if p.host != nil {
		if cfg := p.host.Config(); cfg != nil {
			secret = cfg.TelegramInteractive.SecretToken
		}
	}
	if secret == "" {
		return false // fail closed: no secret configured ⇒ reject everything
	}
	got := r.Header.Get(secretTokenHeader)
	return subtle.ConstantTimeCompare([]byte(got), []byte(secret)) == 1
}

// parseCallbackData splits "<action> <uid>" into its two parts.
func parseCallbackData(data string) (action, uid string) {
	parts := strings.SplitN(strings.TrimSpace(data), " ", 2)
	if len(parts) != 2 {
		return strings.TrimSpace(data), ""
	}
	return parts[0], strings.TrimSpace(parts[1])
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

// editMessageText edits the original message in place. Best-effort: failures are
// logged, not propagated (the state change already happened).
func (p *Plugin) editMessageText(ctx context.Context, chatID, messageID int64, text string) {
	body := map[string]any{
		"chat_id":    chatID,
		"message_id": messageID,
		"text":       text,
	}
	p.callAPI(ctx, "editMessageText", body)
}

// answerCallbackQuery clears the client's loading spinner.
func (p *Plugin) answerCallbackQuery(ctx context.Context, callbackID, text string) {
	body := map[string]any{
		"callback_query_id": callbackID,
		"text":              text,
	}
	p.callAPI(ctx, "answerCallbackQuery", body)
}

// callAPI POSTs a JSON body to {api_base}/bot{token}/{method}. Best-effort.
func (p *Plugin) callAPI(ctx context.Context, method string, body map[string]any) {
	cfg := p.tgConfig()
	if cfg.BotToken == "" {
		return
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return
	}
	url := strings.TrimRight(cfg.APIBase, "/") + "/bot" + cfg.BotToken + "/" + method

	reqCtx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.newClient(defaultTimeout).Do(req)
	if err != nil {
		if lg := p.logger(); lg != nil {
			lg.Warn("telegraminteractive: api call failed", "plugin", p.Name(), "method", method, "err", err)
		}
		return
	}
	_ = resp.Body.Close()
}

// tgConfig reads the TelegramInteractive file-config section from the host.
func (p *Plugin) tgConfig() struct {
	BotToken string
	APIBase  string
} {
	out := struct {
		BotToken string
		APIBase  string
	}{APIBase: "https://api.telegram.org"}
	if p.host != nil {
		if cfg := p.host.Config(); cfg != nil {
			out.BotToken = cfg.TelegramInteractive.BotToken
			if cfg.TelegramInteractive.APIBase != "" {
				out.APIBase = cfg.TelegramInteractive.APIBase
			}
		}
	}
	return out
}

// defaultClient returns a plain http.Client with the given timeout.
func defaultClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &http.Client{Timeout: timeout}
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
