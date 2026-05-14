package teams

import (
	"context"
	"fmt"
	"html"
	"regexp"
	"strings"
	"time"

	"github.com/japannext/snooze/pkg/snoozeclient"
	"github.com/japannext/snooze/pkg/snoozetypes"
)

// botMarker is embedded as an HTML comment in every outbound message so the
// poll loop can recognise its own posts and skip them — Teams does not
// distinguish "this app posted that message" once the message is in the
// channel, so we self-tag.
const botMarker = "<!-- snooze-bot -->"

// htmlTagRE strips HTML tags from a Graph message body when contentType=html
// so the command parser sees plain text.
var htmlTagRE = regexp.MustCompile(`<[^>]*>`)

// whitespaceRE collapses runs of whitespace produced by tag stripping.
var whitespaceRE = regexp.MustCompile(`\s+`)

// commandSplitRE splits an incoming message into <command> <rest>. The
// command is the leading token of alphanumerics; everything else is the
// argument tail. We split on the same character set as the Python bot.
var commandSplitRE = regexp.MustCompile(`[^a-zA-Z0-9/]`)

// stripHTML turns an HTML message body into a plain-text command line. It
// drops tags, unescapes entities, and collapses whitespace.
func stripHTML(s string) string {
	if s == "" {
		return ""
	}
	s = htmlTagRE.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	s = whitespaceRE.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// command is the parsed form of an inbound Teams message. The poll loop
// extracts a command and trailing args; the forwarder turns it into a Snooze
// REST call.
type command struct {
	// Verb is the lower-cased leading token: "ack", "close", "snooze", etc.
	// An empty Verb means the message wasn't a recognised command (treated
	// as a comment in handleCommand).
	Verb string
	// Args is everything after the first delimiter — duration, comment, etc.
	Args string
	// Speaker is the Teams displayName of the author. Recorded as the comment
	// "name" on the Snooze side so audit logs show who triggered what.
	Speaker string
	// ThreadID is the Graph id of the root message (replyToId for replies,
	// otherwise the message's own id). Records correlation across loops.
	ThreadID string
}

// parseCommand converts a Graph chatMessage into a typed command. It returns
// ok=false when the message is empty, an emoji-only reaction, or originated
// from the bot itself.
func parseCommand(msg graphMessage, selfUserID, selfUserName, botName string) (command, bool) {
	if isSelfMessage(msg, selfUserID, selfUserName, botName) {
		return command{}, false
	}
	body := msg.Body.Content
	if strings.EqualFold(msg.Body.ContentType, "html") {
		body = stripHTML(body)
	} else {
		body = strings.TrimSpace(body)
	}
	if body == "" {
		return command{}, false
	}
	// Drop a leading "@<botname>" mention if present — Teams renders it as
	// plain text once HTML is stripped.
	prefix := "@" + botName
	if strings.HasPrefix(strings.ToLower(body), strings.ToLower(prefix)) {
		body = strings.TrimSpace(body[len(prefix):])
	}
	verb := ""
	args := body
	if parts := commandSplitRE.Split(body, 2); len(parts) > 0 {
		verb = strings.ToLower(parts[0])
		if len(parts) == 2 {
			args = strings.TrimSpace(parts[1])
		} else {
			args = ""
		}
	}
	speaker := "unknown"
	if msg.From.User != nil && msg.From.User.DisplayName != "" {
		speaker = msg.From.User.DisplayName
	}
	root := msg.ReplyToID
	if root == "" {
		root = msg.ID
	}
	return command{Verb: verb, Args: args, Speaker: speaker, ThreadID: root}, true
}

// isSelfMessage reports whether msg was posted by the bot itself. We check
// (a) the embedded marker, (b) the application identity (Graph fills `from.application`
// for bot posts), (c) the configured self user id/displayName.
func isSelfMessage(msg graphMessage, selfUserID, selfUserName, botName string) bool {
	if strings.Contains(msg.Body.Content, botMarker) {
		return true
	}
	if msg.From.Application != nil && msg.From.Application.ID != "" {
		return true
	}
	if msg.From.User != nil {
		if selfUserID != "" && msg.From.User.ID == selfUserID {
			return true
		}
		if selfUserName != "" && strings.EqualFold(msg.From.User.DisplayName, selfUserName) {
			return true
		}
		if botName != "" && strings.EqualFold(msg.From.User.DisplayName, botName) {
			return true
		}
	}
	return false
}

// alertPoster is the minimal slice of snoozeclient.Client used by the
// forwarder. Tests inject a fake implementation; production code passes the
// real *snoozeclient.Client.
type alertPoster interface {
	PostAlert(ctx context.Context, rec snoozetypes.Record) (snoozetypes.Record, error)
}

// Compile-time check: snoozeclient.Client satisfies alertPoster.
var _ alertPoster = (*snoozeclient.Client)(nil)

// forwarder owns the inbound-to-snooze bridge: it turns a parsed command into
// a Snooze record (via PostAlert) carrying the action verb in Tags, the
// argument body in Message, and the channel/thread context in Raw.
//
// The Snooze server then routes the synthetic record through its normal alert
// pipeline — rule + aggregaterule + notification plugins — exactly as if it
// had come from an external monitoring system. This keeps the daemon dumb
// (no direct ack/close API calls) and pushes auth + rate limiting into the
// server.
type forwarder struct {
	client    alertPoster
	channelID string
	teamID    string
	source    string
}

// newForwarder wires up the bridge. source becomes the Source field of every
// synthetic record so operators can route Teams-originated commands distinctly
// from other inputs.
func newForwarder(client alertPoster, teamID, channelID string) *forwarder {
	return &forwarder{
		client:    client,
		teamID:    teamID,
		channelID: channelID,
		source:    "teams",
	}
}

// forwardCommand turns cmd into a Snooze record and posts it to /api/v1/alerts.
// The resulting Record has Tags=[verb], Process=verb, Message=args, and the
// Teams thread id stashed in Raw so server-side plugins can correlate.
func (f *forwarder) forwardCommand(ctx context.Context, cmd command) (snoozetypes.Record, error) {
	if cmd.Verb == "" {
		// An empty verb shouldn't reach here (parseCommand returns ok=false
		// for empty messages); guard defensively so a bad input can't post
		// a useless record.
		return snoozetypes.Record{}, fmt.Errorf("teams: empty command")
	}
	rec := snoozetypes.Record{
		Source:    f.source,
		Process:   cmd.Verb,
		Message:   cmd.Args,
		Timestamp: time.Now().UTC(),
		Tags:      []string{cmd.Verb},
		Raw: map[string]any{
			"speaker":    cmd.Speaker,
			"thread_id":  cmd.ThreadID,
			"team_id":    f.teamID,
			"channel_id": f.channelID,
			"verb":       cmd.Verb,
			"args":       cmd.Args,
		},
	}
	return f.client.PostAlert(ctx, rec)
}
