package teams

import (
	"html"
	"regexp"
	"strings"
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
