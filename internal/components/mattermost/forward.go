package mattermost

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/japannext/snooze/pkg/snoozeclient"
)

// CommandKind is the discriminator for the parsed Mattermost slash-command
// dispatched by the daemon. New verbs should be added here so the
// (purely-syntactic) parser stays decoupled from the (network-touching)
// forwarder.
type CommandKind int

const (
	// CmdUnknown means the message did not parse as a recognized verb.
	CmdUnknown CommandKind = iota
	// CmdHelp lists supported verbs back to the user.
	CmdHelp
	// CmdAck acknowledges an alert by UID.
	CmdAck
	// CmdClose closes an alert by UID.
	CmdClose
	// CmdReopen re-opens a previously closed alert.
	CmdReopen
	// CmdComment adds a free-form comment to an alert.
	CmdComment
)

// Command is the parsed representation of a `/snooze <verb> <uid> [msg]` line.
// UID may be empty (for help / malformed input); Forward turns that into an
// error reply.
type Command struct {
	Kind    CommandKind
	UID     string
	Message string
	Raw     string
}

// ParseCommand splits a Mattermost slash-command line into a Command.
// The grammar is intentionally tiny:
//
//	/snooze help
//	/snooze ack    <uid> [free-form message …]
//	/snooze close  <uid> [free-form message …]
//	/snooze reopen <uid> [free-form message …]
//	/snooze comment <uid> <message …>
//
// The leading "/snooze" prefix is optional — the daemon strips it before
// calling ParseCommand so the same code path handles both slash commands
// and at-mentions.
func ParseCommand(line string) Command {
	raw := strings.TrimSpace(line)
	cmd := Command{Raw: raw}
	if raw == "" {
		return cmd
	}
	fields := strings.Fields(raw)
	// Tolerate a leading "/snooze" or "@snooze" prefix.
	if len(fields) > 0 {
		first := strings.ToLower(fields[0])
		if first == "/snooze" || first == "@snooze" || first == "snooze" {
			fields = fields[1:]
		}
	}
	if len(fields) == 0 {
		cmd.Kind = CmdHelp
		return cmd
	}
	verb := strings.ToLower(fields[0])
	rest := fields[1:]
	switch verb {
	case "help", "/help":
		cmd.Kind = CmdHelp
	case "ack", "acknowledge", "ok":
		cmd.Kind = CmdAck
		cmd.UID, cmd.Message = popUID(rest)
	case "close", "done":
		cmd.Kind = CmdClose
		cmd.UID, cmd.Message = popUID(rest)
	case "open", "reopen", "re-open":
		cmd.Kind = CmdReopen
		cmd.UID, cmd.Message = popUID(rest)
	case "comment":
		cmd.Kind = CmdComment
		cmd.UID, cmd.Message = popUID(rest)
	default:
		cmd.Kind = CmdUnknown
	}
	return cmd
}

// popUID extracts the first token as the alert UID and joins the rest into
// a free-form message. Empty input returns ("", "").
func popUID(fields []string) (string, string) {
	if len(fields) == 0 {
		return "", ""
	}
	return fields[0], strings.Join(fields[1:], " ")
}

// HelpText is the reply emitted in response to /snooze help (or an
// unrecognized verb). Kept as a package-level so tests can assert on it.
var HelpText = strings.TrimSpace(`
**Snooze bot** — available commands:

` + "`/snooze ack <uid> [msg]`" + ` — acknowledge an alert
` + "`/snooze close <uid> [msg]`" + ` — close an alert
` + "`/snooze reopen <uid> [msg]`" + ` — re-open an alert
` + "`/snooze comment <uid> <msg>`" + ` — comment on an alert
` + "`/snooze help`" + ` — show this message
`)

// snoozeAPI is the slice of pkg/snoozeclient.Client used by Forward.
// Defined as an interface so tests can stub it without spinning up an
// httptest server for every case.
type snoozeAPI interface {
	Post(ctx context.Context, path string, body, dest any) error
	Do(ctx context.Context, method, path string, body, dest any) error
}

// commentRequest mirrors the wire shape POST /api/v1/comments accepts.
// We keep it loose (map-ish) to avoid coupling the Mattermost component
// to a future commentstypes package.
type commentRequest struct {
	Type      string `json:"type,omitempty"`
	RecordUID string `json:"record_uid"`
	Name      string `json:"name,omitempty"`
	Method    string `json:"method,omitempty"`
	Message   string `json:"message,omitempty"`
}

// Forward executes the user's intent against Snooze and returns the
// Markdown reply that should be posted back into Mattermost. The reply is
// always non-empty so the caller can blindly relay it.
//
// `user` is the Mattermost display-name of the requester — surfaced into
// the Snooze comment record so the audit trail names the human, not the
// bot.
func Forward(ctx context.Context, sc snoozeAPI, c Command, user string) string {
	switch c.Kind {
	case CmdHelp:
		return HelpText
	case CmdUnknown:
		return ":x: Unknown command. Try `/snooze help`."
	case CmdAck, CmdClose, CmdReopen, CmdComment:
		// fallthrough
	default:
		return ":x: Unsupported command."
	}
	if c.UID == "" {
		return ":x: Missing alert UID. Usage: `/snooze " + verbName(c.Kind) + " <uid> [msg]`"
	}
	req := commentRequest{
		Type:      verbName(c.Kind),
		RecordUID: c.UID,
		Name:      user,
		Method:    "mattermost",
		Message:   c.Message,
	}
	// /api/v1/comments is the canonical endpoint exercised by every
	// snooze chat bot — both the legacy Python implementations and the
	// Go client expose it through the generic Post/Do helpers.
	if err := sc.Do(ctx, http.MethodPost, "/api/v1/comments", req, nil); err != nil {
		return fmt.Sprintf(":x: Snooze API rejected `%s %s`: %v", verbName(c.Kind), c.UID, err)
	}
	switch c.Kind {
	case CmdAck:
		return fmt.Sprintf(":white_check_mark: Alert `%s` acknowledged by `%s`.", c.UID, user)
	case CmdClose:
		return fmt.Sprintf(":white_check_mark: Alert `%s` closed by `%s`.", c.UID, user)
	case CmdReopen:
		return fmt.Sprintf(":white_check_mark: Alert `%s` re-opened by `%s`.", c.UID, user)
	case CmdComment:
		return fmt.Sprintf(":white_check_mark: Comment added to alert `%s` by `%s`.", c.UID, user)
	}
	return ":white_check_mark: done."
}

// verbName maps a CommandKind back to its wire-level verb string.
func verbName(k CommandKind) string {
	switch k {
	case CmdAck:
		return "ack"
	case CmdClose:
		return "close"
	case CmdReopen:
		return "open"
	case CmdComment:
		return "comment"
	default:
		return ""
	}
}

// snoozeClientAdapter exposes a *snoozeclient.Client through the snoozeAPI
// interface. Kept as an explicit adapter so changes to the client surface
// don't ripple into the forward.go pure logic.
type snoozeClientAdapter struct{ c *snoozeclient.Client }

func (a snoozeClientAdapter) Post(ctx context.Context, path string, body, dest any) error {
	return a.c.Post(ctx, path, body, dest)
}

func (a snoozeClientAdapter) Do(ctx context.Context, method, path string, body, dest any) error {
	return a.c.Do(ctx, method, path, body, dest)
}
