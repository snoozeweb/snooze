// Package googlechat implements the "googlechat" Notifier plugin: it posts an
// alert to a Google Chat space via an Incoming Webhook URL. The plugin
// supports two body modes — a plain {"text":"..."} message and a structured
// cardsV2 card — and optional reply threading via a threadKey.
//
// net/http only; no SDK. All configuration is carried per-call through
// plugins.NotificationPayload.Meta (action_form values).
//
// Note: the bidirectional Google Chat bot (snooze-googlechat daemon) is a
// separate, in-progress component not covered by this plugin.
package googlechat

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"text/template"
	"time"

	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

//go:embed metadata.yaml
var metaYAML []byte

func init() {
	plugins.Register("googlechat", metaYAML, factory)
}

// defaultTimeout matches the webhook plugin baseline.
const defaultTimeout = 10 * time.Second

// maxResponseBytes caps the body we read for error diagnostics.
const maxResponseBytes = 64 << 10

// defaultMessage is rendered when the operator omits the "message" knob.
const defaultMessage = `*{{ .Severity }}* on {{ .Host }}: {{ .Message }}`

// Plugin is the Google Chat notifier. Send is safe for concurrent calls: every
// call builds its own HTTP client and holds no shared mutable state.
type Plugin struct {
	meta plugins.Metadata
	host plugins.Host

	// newClient is overridable from tests so httptest can intercept. In
	// production defaultClientFn is used.
	newClient func(timeout time.Duration) *http.Client
}

// factory builds the plugin instance.
func factory(meta plugins.Metadata) (plugins.Plugin, error) {
	return &Plugin{meta: meta, newClient: defaultClientFn}, nil
}

// defaultClientFn returns a plain http.Client with the given timeout.
func defaultClientFn(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &http.Client{Timeout: timeout}
}

// Name returns the registry key.
func (p *Plugin) Name() string { return "googlechat" }

// Metadata returns the parsed metadata.yaml.
func (p *Plugin) Metadata() plugins.Metadata { return p.meta }

// PostInit wires the host. There is no DB collection to load.
func (p *Plugin) PostInit(_ context.Context, host plugins.Host) error {
	p.host = host
	if p.newClient == nil {
		p.newClient = defaultClientFn
	}
	return nil
}

// Reload is a no-op: the plugin holds no cached state.
func (p *Plugin) Reload(_ context.Context) error { return nil }

// Send posts a Google Chat message to the configured webhook URL. The Meta map
// carries the action_form field values (webhook_url, message, use_card,
// thread_key, timeout).
//
// On non-2xx the error contains the HTTP status code and a response body
// excerpt. The caller (notification worker) is responsible for retries.
func (p *Plugin) Send(ctx context.Context, rec snoozetypes.Record, payload plugins.NotificationPayload) error {
	cfg, err := configFromMeta(payload.Meta)
	if err != nil {
		return fmt.Errorf("googlechat: config: %w", err)
	}

	// Render the message template.
	msgText, err := renderTemplate("message", cfg.message, rec)
	if err != nil {
		return fmt.Errorf("googlechat: render message: %w", err)
	}

	// Render the thread_key template. It defaults to the alert's hash so every
	// occurrence and re-escalation of one alert lands in one Chat thread
	// instead of starting a new conversation each time.
	threadKey, err := renderTemplate("thread_key", cfg.threadKey, rec)
	if err != nil {
		return fmt.Errorf("googlechat: render thread_key: %w", err)
	}
	threadKey = strings.TrimSpace(threadKey)

	// Build the request URL, appending the threading query parameter when
	// a threadKey is present.
	reqURL := cfg.webhookURL
	if threadKey != "" {
		if strings.Contains(reqURL, "?") {
			reqURL += "&messageReplyOption=REPLY_MESSAGE_FALLBACK_TO_NEW_THREAD"
		} else {
			reqURL += "?messageReplyOption=REPLY_MESSAGE_FALLBACK_TO_NEW_THREAD"
		}
	}

	// A re-escalation posts a short text reply rather than repeating the card:
	// the thread root already shows host / severity / message, so a second full
	// card is noise that buries the one new fact (that this escalated again).
	// Mirrors the Teams bridge's threaded-reply behaviour.
	useCard := cfg.useCard
	if payload.Escalation.IsRe() && threadKey != "" {
		msgText = payload.Escalation.PrefixMessage(msgText)
		useCard = false
	}

	// Build the JSON body.
	body, err := buildBody(rec, msgText, threadKey, useCard)
	if err != nil {
		return fmt.Errorf("googlechat: build body: %w", err)
	}

	timeout := cfg.timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, reqURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("googlechat: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := p.newClient(timeout)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("googlechat: do request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	preview, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("googlechat: HTTP %d: %s", resp.StatusCode, truncate(preview, 200))
	}
	p.recordThread(rec, payload, preview)
	return nil
}

// Compile-time proof we satisfy the contract.
var _ plugins.Notifier = (*Plugin)(nil)

// chatThreadsField is a flat, action-independent list of the Chat thread names
// this alert has been posted into.
//
// It exists so the snooze-googlechat daemon can resolve "which alert does this
// thread belong to?" from an inbound Chat command with ONE search on a field
// whose name it knows in advance. The per-action notify_ref handle cannot serve
// that lookup: the daemon receives a thread name and has no idea which action
// posted it, so it would have to search every notify_ref_* field there might be.
const chatThreadsField = "chat_threads"

// recordThread persists the Chat thread this message landed in, so an operator
// replying in that thread can be matched back to this alert.
//
// Two writes, deliberately:
//
//   - notify_ref_<action>.thread_name, the per-action handle every other
//     notifier uses;
//   - chat_threads, the flat searchable list the daemon queries.
//
// A response without a thread name (an older Chat API shape, a webhook that
// answers with an empty body) leaves both untouched: the notification already
// landed, and the only cost is that an inbound command on that thread will not
// resolve.
func (p *Plugin) recordThread(rec snoozetypes.Record, payload plugins.NotificationPayload, body []byte) {
	var resp struct {
		Thread struct {
			Name string `json:"name"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(body, &resp); err != nil || resp.Thread.Name == "" {
		return
	}
	name := resp.Thread.Name
	actionName := payload.ActionName()

	plugins.StoreNotifyRef(payload, actionName,
		plugins.MergeNotifyRef(rec, actionName, map[string]any{"thread_name": name}))

	// Read-modify-write against the in-memory record. aggregaterule carries
	// chat_threads forward onto every subsequent occurrence, so the list
	// accumulates across an alert's lifetime rather than being reset per fire.
	threads := existingThreads(rec)
	if slices.Contains(threads, name) {
		return
	}
	threads = append(threads, name)
	// Bound the list: an alert posted into many threads over a long life must
	// not grow the record without limit. The newest are the ones an operator is
	// plausibly replying in.
	if len(threads) > maxTrackedThreads {
		threads = threads[len(threads)-maxTrackedThreads:]
	}
	plugins.InjectField(payload.Inject, chatThreadsField, threads)
}

// maxTrackedThreads caps how many Chat thread names one alert accumulates.
const maxTrackedThreads = 20

// existingThreads reads the thread list already on the record, tolerating the
// []any shape a driver round-trip produces.
func existingThreads(rec snoozetypes.Record) []string {
	if rec.Extra == nil {
		return nil
	}
	switch v := rec.Extra[chatThreadsField].(type) {
	case []string:
		return slices.Clone(v)
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

// ---- config -----------------------------------------------------------------

// config holds the per-call knobs decoded from NotificationPayload.Meta.
type config struct {
	webhookURL string
	message    string
	useCard    bool
	threadKey  string
	timeout    time.Duration
}

// defaultThreadKey groups every occurrence and re-escalation of one alert into
// a single Chat thread. Before this was the default, each re-escalation started
// a fresh conversation, so a channel watching a flapping alert filled with
// identical unlinked cards.
const defaultThreadKey = "{{ .Hash }}"

// configFromMeta decodes config from the payload Meta map. Missing fields fall
// back to sensible defaults; a missing webhook_url is a hard error.
func configFromMeta(meta map[string]any) (config, error) {
	cfg := config{
		message:   defaultMessage,
		useCard:   true,
		threadKey: defaultThreadKey,
		timeout:   defaultTimeout,
	}

	if meta == nil {
		return cfg, fmt.Errorf("webhook_url is required")
	}

	if v, ok := meta["webhook_url"].(string); ok {
		cfg.webhookURL = strings.TrimSpace(v)
	}
	if cfg.webhookURL == "" {
		return cfg, fmt.Errorf("webhook_url is required")
	}

	if v, ok := meta["message"].(string); ok && v != "" {
		cfg.message = v
	}

	// use_card may arrive as bool (Switch component) or string "true".
	switch v := meta["use_card"].(type) {
	case bool:
		cfg.useCard = v
	case string:
		cfg.useCard = strings.EqualFold(v, "true")
	}

	// An explicitly configured thread_key wins, including an explicit empty
	// string — an operator who wants every alert as its own conversation must
	// be able to say so.
	if v, ok := meta["thread_key"].(string); ok {
		cfg.threadKey = v
	}

	if d, ok := parseTimeout(meta["timeout"]); ok {
		cfg.timeout = d
	}

	return cfg, nil
}

// ---- body builders ----------------------------------------------------------

// cardBody is the JSON shape Google Chat expects for a cardsV2 message.
type cardBody struct {
	CardsV2 []cardEntry    `json:"cardsV2"`
	Thread  *threadWrapper `json:"thread,omitempty"`
}

// plainBody is the JSON shape for a plain text message.
type plainBody struct {
	Text   string         `json:"text"`
	Thread *threadWrapper `json:"thread,omitempty"`
}

type threadWrapper struct {
	ThreadKey string `json:"threadKey"`
}

type cardEntry struct {
	CardID string   `json:"cardId"`
	Card   cardItem `json:"card"`
}

type cardItem struct {
	Header   cardHeader    `json:"header"`
	Sections []cardSection `json:"sections"`
}

type cardHeader struct {
	Title    string `json:"title"`
	Subtitle string `json:"subtitle"`
}

type cardSection struct {
	Widgets []cardWidget `json:"widgets"`
}

type cardWidget struct {
	DecoratedText decoratedText `json:"decoratedText"`
}

type decoratedText struct {
	Text string `json:"text"`
}

// buildBody returns the JSON-encoded request body for the given mode.
func buildBody(rec snoozetypes.Record, msgText, threadKey string, useCard bool) ([]byte, error) {
	var thread *threadWrapper
	if threadKey != "" {
		thread = &threadWrapper{ThreadKey: threadKey}
	}

	if useCard {
		b := cardBody{
			CardsV2: []cardEntry{
				{
					CardID: "snooze",
					Card: cardItem{
						Header: cardHeader{
							Title:    rec.Host,
							Subtitle: rec.Severity,
						},
						Sections: []cardSection{
							{
								Widgets: []cardWidget{
									{DecoratedText: decoratedText{Text: msgText}},
								},
							},
						},
					},
				},
			},
			Thread: thread,
		}
		return json.Marshal(b)
	}

	pb := plainBody{Text: msgText, Thread: thread}
	return json.Marshal(pb)
}

// ---- template helpers -------------------------------------------------------

// renderTemplate executes a Go text/template over the record fields. When the
// template string is empty, an empty string is returned without error.
func renderTemplate(name, tmpl string, rec snoozetypes.Record) (string, error) {
	if tmpl == "" {
		return "", nil
	}
	if !strings.Contains(tmpl, "{{") {
		// Fast-path: no template directives, return verbatim.
		return tmpl, nil
	}
	t, err := template.New(name).Option("missingkey=zero").Parse(tmpl)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, rec); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// ---- utility ----------------------------------------------------------------

// parseTimeout accepts a duration string, int/float64 seconds, or
// time.Duration. Anything else yields (0, false).
func parseTimeout(v any) (time.Duration, bool) {
	switch x := v.(type) {
	case time.Duration:
		if x > 0 {
			return x, true
		}
	case string:
		d, err := time.ParseDuration(x)
		if err == nil && d > 0 {
			return d, true
		}
	case int:
		if x > 0 {
			return time.Duration(x) * time.Second, true
		}
	case int64:
		if x > 0 {
			return time.Duration(x) * time.Second, true
		}
	case float64:
		if x > 0 {
			return time.Duration(x * float64(time.Second)), true
		}
	}
	return 0, false
}

// truncate returns at most n bytes of b as a string, with an ellipsis when
// the input was longer.
func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}
