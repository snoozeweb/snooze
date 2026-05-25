package googlechat

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
)

// ActionType is the canonical set of commands the bot understands. They map
// 1:1 onto the Snooze v1 record-action verbs except for "snooze" which uses
// a different endpoint and "comment" which is the catch-all for free-form text.
type ActionType string

// ActionACK is the canonical "acknowledge" command.
const (
	ActionACK     ActionType = "ack"
	ActionESC     ActionType = "esc"
	ActionClose   ActionType = "close"
	ActionOpen    ActionType = "open"
	ActionSnooze  ActionType = "snooze"
	ActionComment ActionType = "comment"
	ActionHelp    ActionType = "help"
)

// ParsedCommand is the result of decoding the leading verb from a Chat message.
// Args holds the trailing free-form text after the verb (e.g. the comment
// body or the snooze duration). Empty Verb means the message had no token at
// all and should be treated as a no-op.
type ParsedCommand struct {
	Verb ActionType
	Args string
}

// commandAliases maps every recognized verb (including slash-prefixed variants)
// to its canonical ActionType. Mirrors the Python `process_user_message` switch.
var commandAliases = map[string]ActionType{
	"ack":          ActionACK,
	"acknowledge":  ActionACK,
	"ok":           ActionACK,
	"/ack":         ActionACK,
	"esc":          ActionESC,
	"escalate":     ActionESC,
	"re-escalate":  ActionESC,
	"reescalate":   ActionESC,
	"re-esc":       ActionESC,
	"reesc":        ActionESC,
	"/esc":         ActionESC,
	"close":        ActionClose,
	"done":         ActionClose,
	"/close":       ActionClose,
	"open":         ActionOpen,
	"reopen":       ActionOpen,
	"re-open":      ActionOpen,
	"/open":        ActionOpen,
	"snooze":       ActionSnooze,
	"/snooze":      ActionSnooze,
	"help":         ActionHelp,
	"/help":        ActionHelp,
	"help_snooze":  ActionHelp,
	"/help_snooze": ActionHelp,
}

// ChatEvent is the subset of the Google Chat Pub/Sub envelope the forwarder
// reads. The Chat API emits much richer payloads but the bot only needs the
// event type, the originating thread/space and the text the user typed.
//
// Reference (Python): components/googlechat/src/snooze_googlechat/main.py
type ChatEvent struct {
	Type    string      `json:"type"`
	User    ChatUser    `json:"user"`
	Message ChatMessage `json:"message"`
	// Action is populated when type == "CARD_CLICKED"; the actionMethodName
	// effectively becomes the verb.
	Action *ChatAction `json:"action,omitempty"`
}

// ChatUser is the Google Chat user identity embedded in a ChatEvent.
type ChatUser struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
}

// ChatMessage is the message payload within a ChatEvent.
type ChatMessage struct {
	Text         string      `json:"text"`
	ArgumentText string      `json:"argumentText"`
	SlashCommand interface{} `json:"slashCommand,omitempty"`
	Thread       ChatThread  `json:"thread"`
}

// ChatThread holds the thread resource name from a ChatMessage.
type ChatThread struct {
	// Name has the form "spaces/{space}/threads/{thread}".
	Name string `json:"name"`
}

// ChatAction is the action payload for CARD_CLICKED events.
type ChatAction struct {
	ActionMethodName string `json:"actionMethodName"`
}

// rawText reconstructs the message the user typed, mirroring the Python
// precedence: slashCommand → argumentText → text. Whitespace is trimmed.
// CARD_CLICKED events synthesise text from actionMethodName.
func (e ChatEvent) rawText() string {
	if e.Type == "CARD_CLICKED" && e.Action != nil {
		return strings.TrimSpace(e.Action.ActionMethodName)
	}
	if e.Message.SlashCommand != nil && e.Message.Text != "" {
		return strings.TrimSpace(e.Message.Text)
	}
	if e.Message.ArgumentText != "" {
		return strings.TrimSpace(e.Message.ArgumentText)
	}
	return strings.TrimSpace(e.Message.Text)
}

// ParseCommand splits a raw message into a (verb, args) pair using the same
// rules as the Python parser: the first run of alphanumeric / "/" characters
// becomes the command; everything after the first non-matching character is
// the args string. Unknown verbs collapse to ActionComment so free-form text
// still flows through the bot as a comment on the alert.
func ParseCommand(raw string) ParsedCommand {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ParsedCommand{}
	}
	// Find the first character that isn't [A-Za-z0-9/]. Everything before it
	// is the verb; everything after (sans the separator) is args.
	cut := len(raw)
	for i, r := range raw {
		if r != '/' && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
			cut = i
			break
		}
	}
	verb := strings.ToLower(raw[:cut])
	args := ""
	if cut < len(raw) {
		args = strings.TrimSpace(raw[cut+1:])
	}
	if action, ok := commandAliases[verb]; ok {
		return ParsedCommand{Verb: action, Args: args}
	}
	// Unknown verb → treat the entire message as a comment body.
	return ParsedCommand{Verb: ActionComment, Args: raw}
}

// recordSearchHit is the slice of a Snooze record the daemon needs to
// dispatch an action. Only UID is required; hash is kept for logging.
type recordSearchHit struct {
	UID  string `json:"uid"`
	Hash string `json:"hash,omitempty"`
}

// SnoozeClient is the subset of pkg/snoozeclient.Client used by the forwarder.
// It exists to keep the unit-test boundary small — tests inject a fake.
type SnoozeClient interface {
	Post(ctx context.Context, path string, body, dest any) error
}

// commentPayload mirrors the Python `client.comment_batch` body and the Go
// server's POST /api/v1/comment shape.
type commentPayload struct {
	Type      string                   `json:"type,omitempty"`
	RecordUID string                   `json:"record_uid"`
	Name      string                   `json:"name"`
	Method    string                   `json:"method"`
	Message   string                   `json:"message,omitempty"`
	Mods      []map[string]interface{} `json:"modifications,omitempty"`
}

// commentEndpoint is the v1 batch comment endpoint where the comment plugin
// mounts CRUD (p.Name() = "comment"). POSTing a typed comment here triggers
// the AfterCreate hook that applies the state transition on the linked record.
const commentEndpoint = "/api/v1/comment"

// recordSearchEndpoint is the canonical structured-search route. Body shape:
// `{"condition": <Cond>}`. We mirror the Python query (IN thread
// snooze_webhook_responses.content.threads) with a nested-IN condition:
// for every entry in snooze_webhook_responses, check whether content.threads
// contains the thread name we're looking for.
//
// TODO: snooze_webhook_responses is read here and in the jira component but
// nothing in this codebase writes it yet — the googlechat notifier plugin
// that would record the thread name after sending a Chat message has not
// been ported from Python. Until that lands this lookup will return zero
// hits in any real deployment, and the user-facing reply will be "cannot
// find the corresponding alert!".
const recordSearchEndpoint = "/api/v1/record/search"

// Forwarder owns the business logic that turns a ChatEvent into one or more
// Snooze record-action calls plus a reply string suitable for posting back to
// the originating Chat thread.
type Forwarder struct {
	Client  SnoozeClient
	BotName string
	BaseURL string // public URL used to build [Link] hrefs
	Logger  *slog.Logger
}

// NewForwarder constructs a Forwarder with sane defaults.
func NewForwarder(client SnoozeClient, botName, baseURL string, logger *slog.Logger) *Forwarder {
	if logger == nil {
		logger = slog.Default()
	}
	if botName == "" {
		botName = "Snooze"
	}
	return &Forwarder{Client: client, BotName: botName, BaseURL: strings.TrimRight(baseURL, "/"), Logger: logger}
}

// Handle parses ev, dispatches the appropriate Snooze action, and returns the
// human-readable reply the daemon should post back to the originating thread.
//
// Errors are returned only for unrecoverable problems (e.g. malformed payload);
// "user typed something wrong" cases come back as an ordinary reply string so
// the Pub/Sub message can still be ack'd.
func (f *Forwarder) Handle(ctx context.Context, ev ChatEvent) (string, error) {
	if ev.Type != "MESSAGE" && ev.Type != "CARD_CLICKED" {
		// Ignore ADDED_TO_SPACE / REMOVED_FROM_SPACE etc. — the bot stays quiet.
		return "", nil
	}
	cmd := ParseCommand(ev.rawText())
	display := ev.User.DisplayName
	if display == "" {
		display = "anonymous"
	}

	switch cmd.Verb {
	case ActionHelp, "":
		return f.helpText(display, cmd.Args), nil
	case ActionSnooze:
		// The snooze command operates on a different endpoint and accepts a
		// duration in hours. We surface a clear "not yet implemented" reply
		// rather than silently no-op so operators know the limit.
		return f.snoozeReply(display, cmd.Args), nil
	}

	thread := ev.Message.Thread.Name
	if thread == "" {
		return f.errorReply(display, "no thread context"), nil
	}

	hits, err := f.lookupRecords(ctx, thread)
	if err != nil {
		return f.errorReply(display, fmt.Sprintf("lookup alerts: %v", err)), nil
	}
	if len(hits) == 0 {
		return fmt.Sprintf(":x: `%s`: cannot find the corresponding alert! (command: `%s`)", display, ev.rawText()), nil
	}

	user := fmt.Sprintf("%s via %s", display, f.BotName)
	switch cmd.Verb {
	case ActionACK:
		return f.dispatchSimple(ctx, hits, "ack", user, cmd.Args, "acknowledged"), nil
	case ActionClose:
		return f.dispatchSimple(ctx, hits, "close", user, cmd.Args, "closed"), nil
	case ActionOpen:
		return f.dispatchSimple(ctx, hits, "open", user, cmd.Args, "re-opened"), nil
	case ActionESC:
		return f.dispatchSimple(ctx, hits, "esc", user, cmd.Args, "re-escalated"), nil
	case ActionComment:
		return f.dispatchSimple(ctx, hits, "", user, cmd.Args, "commented"), nil
	default:
		return f.errorReply(display, fmt.Sprintf("unknown verb %q", cmd.Verb)), nil
	}
}

// lookupRecords resolves the records associated with the given thread name.
// It POSTs a structured condition to /api/v1/record/search that says:
//
//	IN snooze_webhook_responses where content.threads contains <thread>
//
// The wire form is the legacy nested-list shape the server's condition
// parser accepts; the outer IN's value is itself a list-form condition,
// which the IN evaluator runs against each element of the
// snooze_webhook_responses array.
func (f *Forwarder) lookupRecords(ctx context.Context, thread string) ([]recordSearchHit, error) {
	if thread == "" {
		return nil, errors.New("empty thread")
	}
	body := map[string]any{
		"condition": []any{
			"IN",
			[]any{"IN", thread, "content.threads"},
			"snooze_webhook_responses",
		},
	}
	type envelope struct {
		Data []recordSearchHit `json:"data"`
	}
	var env envelope
	if err := f.Client.Post(ctx, recordSearchEndpoint, body, &env); err != nil {
		return nil, err
	}
	return env.Data, nil
}

// dispatchSimple posts a comment-batch action for every hit. actionType is the
// "type" field in the comment payload (empty for plain comments). pastVerb is
// the verb embedded in the success reply ("acknowledged", "closed", …).
func (f *Forwarder) dispatchSimple(ctx context.Context, hits []recordSearchHit, actionType, user, message, pastVerb string) string {
	payload := make([]commentPayload, 0, len(hits))
	for _, h := range hits {
		payload = append(payload, commentPayload{
			Type:      actionType,
			RecordUID: h.UID,
			Name:      user,
			Method:    "google",
			Message:   message,
		})
	}
	if err := f.Client.Post(ctx, commentEndpoint, payload, nil); err != nil {
		if f.Logger != nil {
			f.Logger.Warn("googlechat: dispatch failed", slog.String("action", actionType), slog.Any("err", err))
		}
		return fmt.Sprintf(":x: could not %s alert(s): %v", pastVerb, err)
	}
	suffix := ""
	if message != "" && actionType != "" {
		suffix = fmt.Sprintf(" with message `%s`", message)
	}
	if len(hits) == 1 {
		return fmt.Sprintf(":white_check_mark: alert %s successfully by `%s`%s!", pastVerb, user, suffix)
	}
	return fmt.Sprintf(":white_check_mark: *%d* alerts %s successfully by `%s`%s!", len(hits), pastVerb, user, suffix)
}

func (f *Forwarder) helpText(displayName, topic string) string {
	if strings.EqualFold(strings.TrimSpace(topic), "snooze") {
		return fmt.Sprintf("`%s`: Command: *@%s* snooze <hours>\n\n*hours* (1-24): _How long to snooze matching alerts._\n\nExample: _@%s_ *snooze* 6", displayName, f.BotName, f.BotName)
	}
	return fmt.Sprintf(`%s: list of available commands:

*ack, acknowledge, ok* [message]: _Acknowledge an alert_
*esc, escalate, re-escalate* [message]: _Re-escalate an alert_
*close, done* [message]: _Close an alert_
*open, reopen, re-open* [message]: _Re-open an alert_
*snooze* <hours>: _Snooze an alert (1-24h)_
any other message: _Comment on an alert_`, displayName)
}

func (f *Forwarder) snoozeReply(displayName, args string) string {
	return fmt.Sprintf(":hourglass: `%s`: snooze command received with args `%s` — not yet wired to the v1 snooze endpoint in this build, see TODO.", displayName, args)
}

func (f *Forwarder) errorReply(displayName, why string) string {
	return fmt.Sprintf(":x: `%s`: %s", displayName, why)
}

