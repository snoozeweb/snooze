package teams

// handler.go owns the inbound Teams → Snooze action dispatch.
//
// Flow:
//
//	pollOnce → parseCommand → handler.Handle
//	          handler resolves (channel, thread_root) → record_uid via the
//	          thread cache, calls the snoozeclient (PostComment /
//	          CreateSnooze), then posts a confirmation reply in the same
//	          thread via the Graph client.
//
// Conceptually this replaces the old forwarder.forwardCommand path. That
// path POSTed a synthetic record back into /api/v1/alerts and trusted some
// downstream rule to convert it into an action — but no such rule was ever
// wired, so chat commands silently dropped. The new path acts directly via
// the REST surface, matching the Python plugin's behaviour.

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/snoozeweb/snooze/pkg/snoozeclient"
)

// commentClient is the slice of snoozeclient.Client the handler needs.
// Defined as an interface so tests can pass a stub without spinning up the
// full HTTP client + server pair.
type commentClient interface {
	PostComment(ctx context.Context, c snoozeclient.Comment) error
	CreateSnooze(ctx context.Context, s snoozeclient.Snooze) error
}

// replyPoster is the slice of *graphClient handler needs. Same rationale as
// commentClient — gives tests a seam without standing up an httptest
// server for the Graph API.
type replyPoster interface {
	sendMessage(ctx context.Context, teamID, channelID, htmlBody string, opts sendOpts) (graphMessage, error)
}

// handler turns a parsed chat command into a Snooze action plus an in-thread
// reply. It is stateless apart from the references it holds; concurrent
// Handle calls are safe.
type handler struct {
	cli        commentClient
	graph      replyPoster
	cache      *threadCache
	teamID     string
	channelID  string
	channelRef string // "teams/<team>/channels/<channel>" — cache lookups
	botName    string
	logger     *slog.Logger
}

// newHandler wires the dependencies. teamID/channelID double as both the
// reply target and as the channel half of the threadCache key (encoded
// into channelRef once, so we don't reformat per call).
func newHandler(cli commentClient, gc replyPoster, cache *threadCache, teamID, channelID, botName string, logger *slog.Logger) *handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &handler{
		cli:        cli,
		graph:      gc,
		cache:      cache,
		teamID:     teamID,
		channelID:  channelID,
		channelRef: "teams/" + teamID + "/channels/" + channelID,
		botName:    botName,
		logger:     logger,
	}
}

// Handle dispatches one parsed command. The boolean return is ok=true when
// the command was something the bridge knows about (ack / snooze / …); a
// blank verb or pure noise returns ok=false so the caller can skip the
// follow-up bookkeeping. Errors are logged + posted back to the channel as
// a reply — they don't propagate up so a single bad command can't crash
// the poller.
func (h *handler) Handle(ctx context.Context, cmd command) bool {
	if cmd.Verb == "" {
		return false
	}
	verb := strings.ToLower(cmd.Verb)
	switch verb {
	case "help", "/help":
		h.reply(ctx, cmd.ThreadID, helpText(h.botName))
		return true
	}
	uid := h.cache.Get(h.channelRef, cmd.ThreadID)
	if uid == "" {
		h.reply(ctx, cmd.ThreadID, fmt.Sprintf(
			"❌ `%s`: I don't recognise this thread — the bridge may have been restarted since the alert landed. Re-fire the alert (or open it in Snooze) and try again.",
			h.escape(cmd.Speaker)))
		return true
	}
	user := h.escape(cmd.Speaker) + " via Teams"
	args := cmd.Args
	method := "teams"

	switch verb {
	case "ack", "acknowledge", "ok", "/ack":
		err := h.cli.PostComment(ctx, snoozeclient.Comment{
			RecordUID: uid,
			Name:      user,
			Method:    method,
			Type:      "ack",
			Message:   args,
		})
		h.replyOnce(ctx, cmd.ThreadID, "acknowledged", "ack", cmd.Speaker, args, err)
		return true
	case "close", "done", "/close":
		err := h.cli.PostComment(ctx, snoozeclient.Comment{
			RecordUID: uid,
			Name:      user,
			Method:    method,
			Type:      "close",
			Message:   args,
		})
		h.replyOnce(ctx, cmd.ThreadID, "closed", "close", cmd.Speaker, args, err)
		return true
	case "open", "reopen", "re-open", "/open":
		err := h.cli.PostComment(ctx, snoozeclient.Comment{
			RecordUID: uid,
			Name:      user,
			Method:    method,
			Type:      "open",
			Message:   args,
		})
		h.replyOnce(ctx, cmd.ThreadID, "re-opened", "open", cmd.Speaker, args, err)
		return true
	case "esc", "escalate", "re-escalate", "reescalate", "re-esc", "reesc", "/esc":
		mods, msg := parseModifications(args)
		err := h.cli.PostComment(ctx, snoozeclient.Comment{
			RecordUID:     uid,
			Name:          user,
			Method:        method,
			Type:          "esc",
			Message:       msg,
			Modifications: mods,
		})
		h.replyEsc(ctx, cmd.ThreadID, cmd.Speaker, mods, msg, err)
		return true
	case "snooze", "/snooze":
		h.handleSnooze(ctx, uid, user, cmd, args)
		return true
	}
	// Default — treat as a free-form comment. Includes "/comment" and any
	// unrecognised verb (so a typo doesn't silently drop the message).
	fullText := cmd.Verb
	if args != "" {
		fullText += " " + args
	}
	if verb == "/comment" {
		fullText = args
	}
	err := h.cli.PostComment(ctx, snoozeclient.Comment{
		RecordUID: uid,
		Name:      user,
		Method:    method,
		Message:   fullText,
	})
	if err != nil {
		h.reply(ctx, cmd.ThreadID, fmt.Sprintf("❌ `%s`: Could not comment alert: %s",
			h.escape(cmd.Speaker), h.escape(err.Error())))
		return true
	}
	h.reply(ctx, cmd.ThreadID, fmt.Sprintf("✅ Comment added by `%s`: `%s`",
		h.escape(cmd.Speaker), h.escape(fullText)))
	return true
}

// handleSnooze parses the duration tail, creates the snooze entry, and
// piggybacks an ack so the record's state flips to acknowledged in the
// same operation (matching the Python flow). The reply states whether the
// snooze is finite or "Forever".
func (h *handler) handleSnooze(ctx context.Context, uid, user string, cmd command, args string) {
	duration, until, _, err := parseSnoozeArgs(args)
	if err != nil {
		h.reply(ctx, cmd.ThreadID, fmt.Sprintf(
			"❌ `%s`: Invalid Snooze duration. Try `snooze 6h`, `snooze 30m`, or `snooze forever`.",
			h.escape(cmd.Speaker)))
		return
	}
	now := time.Now()
	name := fmt.Sprintf("[%s] %s (%s)", duration, h.escape(cmd.Speaker), shortUID())
	snooze := snoozeclient.Snooze{
		Name:            name,
		Comment:         h.escape(cmd.Speaker),
		TimeConstraints: timeConstraints(now, until),
		// Filter to this record's uid so the snooze only suppresses
		// re-deliveries of THIS alert. The Python plugin used `hash` for
		// the same reason, but the new pipeline normalises records by
		// uid; the Snooze condition evaluator accepts either field.
		Condition: []any{"=", "uid", uid},
	}
	if err := h.cli.CreateSnooze(ctx, snooze); err != nil {
		h.reply(ctx, cmd.ThreadID, fmt.Sprintf(
			"❌ `%s`: Could not Snooze alert: %s", h.escape(cmd.Speaker), h.escape(err.Error())))
		return
	}
	// Best-effort ack so the record visibly changes state. Errors here are
	// non-fatal: the snooze entry itself is the load-bearing side-effect.
	_ = h.cli.PostComment(ctx, snoozeclient.Comment{
		RecordUID: uid,
		Name:      user,
		Method:    "teams",
		Type:      "ack",
		Message:   "Snoozed for " + duration,
	})
	if until == nil {
		h.reply(ctx, cmd.ThreadID, fmt.Sprintf(
			"✅ Snoozed forever by `%s`.", h.escape(cmd.Speaker)))
	} else {
		h.reply(ctx, cmd.ThreadID, fmt.Sprintf(
			"✅ Snoozed for **%s** by `%s` — expires at %s.",
			duration, h.escape(cmd.Speaker), until.Local().Format(alertTimestampFormat)))
	}
}

// replyOnce centralises the ack/close/open feedback loop. verb is the
// state-change label that shows in the reply ("acknowledged" / "closed" /
// "re-opened"); recoveryHint is the chat-command verb the user typed
// (used in the error path so they know which action failed).
func (h *handler) replyOnce(ctx context.Context, threadID, label, recoveryHint, speaker, message string, err error) {
	if err != nil {
		h.reply(ctx, threadID, fmt.Sprintf(
			"❌ `%s`: Could not %s alert: %s",
			h.escape(speaker), recoveryHint, h.escape(err.Error())))
		return
	}
	extra := ""
	if message != "" {
		extra = " with message `" + h.escape(message) + "`"
	}
	h.reply(ctx, threadID, fmt.Sprintf(
		"✅ Alert %s by `%s`%s.", label, h.escape(speaker), extra))
}

// replyEsc renders the esc-specific confirmation, listing the parsed
// modifications and the trailing comment (if any).
func (h *handler) replyEsc(ctx context.Context, threadID, speaker string, mods [][]any, message string, err error) {
	if err != nil {
		h.reply(ctx, threadID, fmt.Sprintf(
			"❌ `%s`: Could not re-escalate alert: %s",
			h.escape(speaker), h.escape(err.Error())))
		return
	}
	parts := []string{fmt.Sprintf("✅ Alert re-escalated by `%s`", h.escape(speaker))}
	if len(mods) > 0 {
		var ms []string
		for _, m := range mods {
			ms = append(ms, fmt.Sprintf("`%v`", m))
		}
		parts = append(parts, "with modifications "+strings.Join(ms, ", "))
	}
	if message != "" {
		parts = append(parts, "and message `"+h.escape(message)+"`")
	}
	h.reply(ctx, threadID, strings.Join(parts, " ")+".")
}

// reply posts text as an HTML chatMessage in the channel, threaded under
// rootID. Errors are logged but not surfaced: the action has already been
// applied by the time we reply, and a failed reply doesn't change the
// underlying record state.
func (h *handler) reply(ctx context.Context, rootID, text string) {
	if h.graph == nil || rootID == "" {
		return
	}
	body := "<p>" + text + "</p>" + botMarker
	if _, err := h.graph.sendMessage(ctx, h.teamID, h.channelID, body, sendOpts{ReplyToID: rootID}); err != nil {
		h.logger.Warn("teams: reply post failed", slog.String("thread", rootID), slog.Any("err", err))
	}
}

// escape strips HTML metacharacters from operator-controlled fragments
// that land back in a Teams chat body. snooze-teams trusts the channel
// (only authenticated members can post), so this is a defense-in-depth
// measure rather than a sanitiser for hostile input.
func (h *handler) escape(s string) string {
	r := strings.NewReplacer(
		"<", "&lt;",
		">", "&gt;",
		"&", "&amp;",
	)
	return r.Replace(s)
}

// helpText returns the multi-line help reply rendered for `help` / `/help`.
// Format mirrors the Python plugin's text so operators familiar with the
// old bot can read it without retraining.
func helpText(botName string) string {
	if botName == "" {
		botName = "SnoozeBot"
	}
	return strings.Join([]string{
		"List of available commands (mention me with `@" + botName + "` or type the verb directly):",
		"",
		"• **ack**, **acknowledge**, **ok** [message] — acknowledge an alert",
		"• **close**, **done** [message] — close an alert",
		"• **open**, **reopen** [message] — re-open an alert",
		"• **snooze** &lt;duration&gt; — snooze an alert (e.g. `snooze 6h`, `snooze forever`)",
		"• **esc**, **escalate** &lt;field=value …&gt; [message] — re-escalate an alert",
		"• any other text — adds a plain comment",
	}, "<br>")
}

// shortUID returns the first 5 hex digits of a fresh UUID-like blob — the
// random tail the Python plugin appended to snooze entry names so operators
// could distinguish multiple snoozes targeting the same record. We use
// time-derived entropy rather than crypto/rand to keep the helper trivial;
// collisions are harmless (the snooze server doesn't enforce name
// uniqueness on this collection).
func shortUID() string {
	const hex = "0123456789abcdef"
	n := time.Now().UnixNano()
	var buf [5]byte
	for i := range buf {
		buf[i] = hex[n&0xF]
		n >>= 4
	}
	if !utf8.ValidString(string(buf[:])) {
		// Can't happen — hex digits are ASCII — but keeps the function
		// total in the eyes of static analysis.
		return "00000"
	}
	return string(buf[:])
}
