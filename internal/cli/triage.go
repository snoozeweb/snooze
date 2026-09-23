package cli

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/pkg/snoozeclient"
)

// recordListFilter holds the `snooze record list` filter flags. build turns
// them into one server-side condition; every flag left unset contributes
// nothing, so no flag at all yields the zero (match-all) condition.
type recordListFilter struct {
	active    bool
	states    []string
	host      string
	severity  string
	owner     string
	condition string
}

func (f *recordListFilter) register(c *cobra.Command) {
	c.Flags().BoolVar(&f.active, "active", false,
		"Only alerts needing attention (the web Alerts tab: not ack/closed/shelved/snoozed)")
	c.Flags().StringSliceVar(&f.states, "state", nil, "Only these states: open, ack, esc, close, shelved")
	c.Flags().StringVar(&f.host, "host", "", "Only this host")
	c.Flags().StringVar(&f.severity, "severity", "", "Only this severity")
	c.Flags().StringVar(&f.owner, "owner", "", "Only alerts owned by: me, none (unowned), or a login")
	c.Flags().StringVarP(&f.condition, "condition", "c", "", "Extra JSON condition (same syntax as `snooze query`)")
}

// recordStates are the values --state accepts. "open" is the posture of an
// alert nobody has touched (no state at all) as well as a re-opened one.
var recordStates = map[string]bool{"open": true, "ack": true, "esc": true, "close": true, "shelved": true}

// activeAlerts mirrors the web Alerts tab (web/src/features/alerts/tabs.ts
// ACTIVE_ALERTS): not acknowledged, closed, snoozed or shelved (timed shelve
// state, or a permanent shelve's negative ttl). Keep the two in step.
func activeAlerts() condition.Cond {
	return condition.And(
		condition.Not(condition.Equals("state", "ack")),
		condition.Not(condition.Equals("state", "close")),
		condition.Not(condition.Exists("snoozed")),
		condition.Not(condition.Equals("state", "shelved")),
		condition.Not(condition.Cond{Op: condition.OpLt, Field: "ttl", Value: 0}),
	)
}

// unowned matches an alert with no current owner: the key absent, or cleared
// to "" (ownership is cleared with an explicit empty value, never unset).
func unowned() condition.Cond {
	return condition.Or(condition.Not(condition.Exists("owner")), condition.Equals("owner", ""))
}

func (f *recordListFilter) build(rt *runtime, cl *snoozeclient.Client) (condition.Cond, error) {
	var parts []condition.Cond
	if f.active {
		parts = append(parts, activeAlerts())
	}
	if len(f.states) > 0 {
		var alts []condition.Cond
		for _, s := range f.states {
			s = strings.TrimSpace(strings.ToLower(s))
			if !recordStates[s] {
				return condition.Cond{}, fmt.Errorf("--state %q: must be one of open, ack, esc, close, shelved", s)
			}
			if s == "open" {
				alts = append(alts, condition.Not(condition.Exists("state")),
					condition.Equals("state", ""), condition.Equals("state", "open"))
				continue
			}
			alts = append(alts, condition.Equals("state", s))
		}
		parts = append(parts, condition.Or(alts...))
	}
	if f.host != "" {
		parts = append(parts, condition.Equals("host", f.host))
	}
	if f.severity != "" {
		parts = append(parts, condition.Equals("severity", f.severity))
	}
	switch f.owner {
	case "":
	case "none":
		parts = append(parts, unowned())
	case "me":
		login, err := currentLogin(rt, cl)
		if err != nil {
			return condition.Cond{}, err
		}
		parts = append(parts, condition.Equals("owner", login))
	default:
		parts = append(parts, condition.Equals("owner", f.owner))
	}
	if strings.TrimSpace(f.condition) != "" {
		var extra condition.Cond
		if err := json.Unmarshal([]byte(f.condition), &extra); err != nil {
			return condition.Cond{}, fmt.Errorf("invalid --condition JSON: %w", err)
		}
		parts = append(parts, extra)
	}
	if len(parts) == 0 {
		return condition.Cond{}, nil
	}
	return condition.And(parts...), nil
}

// currentLogin is the login the CLI acts as: --user / the configured username
// when set, else the subject of the bearer token. The token is only decoded,
// not verified — it is our own credential and the server verifies it on every
// call; all we need is the name to filter on.
func currentLogin(rt *runtime, cl *snoozeclient.Client) (string, error) {
	if rt.flags != nil && rt.flags.User != "" {
		return rt.flags.User, nil
	}
	if cl != nil {
		if sub := jwtSubject(cl.Token()); sub != "" {
			return sub, nil
		}
	}
	return "", errors.New("--owner me: cannot tell who you are; set --user (or SNOOZE_USER) or log in first")
}

// jwtSubject returns the `sub` claim of a JWT, or "" when tok is not one.
func jwtSubject(tok string) string {
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims struct {
		Sub string `json:"sub"`
	}
	if json.Unmarshal(payload, &claims) != nil {
		return ""
	}
	return claims.Sub
}

// newRecordReopenCmd implements `snooze record reopen <uid> [-m msg]`.
func newRecordReopenCmd() *cobra.Command {
	var message string
	c := &cobra.Command{
		Use:   "reopen <uid>",
		Short: "Re-open an acknowledged or closed alert (clears its owner)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return postRecordTransition(cmd, args[0], "open", message, "Re-opened via snooze CLI")
		},
	}
	c.Flags().StringVarP(&message, "message", "m", "", "Comment message (defaults to a generic note)")
	return c
}

// newRecordEscalateCmd implements `snooze record escalate <uid> [-m msg]`.
func newRecordEscalateCmd() *cobra.Command {
	var message string
	c := &cobra.Command{
		Use:   "escalate <uid>",
		Short: "Re-escalate an acknowledged alert: notify again and clear its owner",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return postRecordTransition(cmd, args[0], "esc", message, "Re-escalated via snooze CLI")
		},
	}
	c.Flags().StringVarP(&message, "message", "m", "", "Comment message (defaults to a generic note)")
	return c
}

// newRecordCommentCmd implements `snooze record comment <uid> -m <text>`: a
// free-form note on the alert's timeline, with no state or ownership change.
func newRecordCommentCmd() *cobra.Command {
	var message string
	c := &cobra.Command{
		Use:   "comment <uid> -m <text>",
		Short: "Add a note to an alert's timeline (no state change)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(message) == "" {
				return errors.New("a comment needs a message: -m '<text>'")
			}
			return postRecordComment(cmd, args[0], "comment", message, "", nil)
		},
	}
	c.Flags().StringVarP(&message, "message", "m", "", "The note (required)")
	return c
}

// newRecordCommentsCmd implements `snooze record comments <uid>`: the alert's
// timeline, oldest first — who acked, assigned, closed, what they wrote, and
// the automatic lifecycle events.
func newRecordCommentsCmd() *cobra.Command {
	var limit int
	c := &cobra.Command{
		Use:   "comments <uid>",
		Short: "Show an alert's timeline (comments and lifecycle events), oldest first",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := runtimeFrom(cmd.Context())
			cl, err := rt.buildClient()
			if err != nil {
				return err
			}
			raw, err := json.Marshal(condition.Equals("record_uid", args[0]))
			if err != nil {
				return err
			}
			path, err := buildQueryPath("comment", string(raw), limit, 0, "date_epoch", "true")
			if err != nil {
				return err
			}
			var resp struct {
				Data []map[string]any `json:"data"`
			}
			if err := cl.Get(cmd.Context(), path, &resp); err != nil {
				return err
			}
			if rt.flags != nil && rt.flags.JSON {
				return renderAny(cmd, rt, resp.Data)
			}
			if len(resp.Data) == 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "(no comments)")
				return nil
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			defer tw.Flush() //nolint:errcheck
			_, _ = fmt.Fprintln(tw, "date\ttype\tby\tmessage")
			for _, c := range resp.Data {
				_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
					commentDate(c), commentType(c), commentAuthor(c), commentText(c))
			}
			return nil
		},
	}
	c.Flags().IntVarP(&limit, "limit", "n", 200, "Maximum number of entries")
	return c
}

// commentDate renders a comment's date_epoch in UTC, falling back to the
// stored human-readable date.
func commentDate(c map[string]any) string {
	var epoch int64
	switch v := c["date_epoch"].(type) {
	case float64:
		epoch = int64(v)
	case int64:
		epoch = v
	}
	if epoch > 0 {
		return time.Unix(epoch, 0).UTC().Format("2006-01-02 15:04Z")
	}
	s, _ := c["date"].(string)
	return s
}

func commentType(c map[string]any) string {
	if t, _ := c["type"].(string); t != "" {
		return t
	}
	return "comment"
}

// commentAuthor names who wrote a comment; user-less (automatic) lifecycle
// comments read as "system", as they do in the web timeline.
func commentAuthor(c map[string]any) string {
	if u, _ := c["user"].(string); u != "" {
		return u
	}
	return "system"
}

// commentText is the one-line message, with the assignee spelled out on an
// assign comment (its message is optional).
func commentText(c map[string]any) string {
	msg, _ := c["message"].(string)
	msg = strings.Join(strings.Fields(msg), " ")
	if commentType(c) == "assign" {
		if who, _ := c["assignee"].(string); who != "" {
			if msg == "" {
				return "→ " + who
			}
			return "→ " + who + ": " + msg
		}
	}
	return msg
}
