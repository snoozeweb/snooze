package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/snoozeweb/snooze/pkg/snoozeclient"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// newRecordCmd builds the `snooze record …` subtree: `post <json>` to ingest
// an alert, `list` to fetch recent records, `show <uid>` to inspect one
// record, `ack` / `close` / `reopen` / `escalate` to drive state transitions,
// `assign` / `release` to change ownership and `comment` to add a note (all
// via the comment endpoint), `comments` to read the timeline, `owners` to count
// alerts per owner, and `bulk` for the query-wide variants.
func newRecordCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "record",
		Short: "Manage records (alerts)",
	}
	cmd.AddCommand(
		newRecordPostCmd(),
		newRecordListCmd(),
		newRecordShowCmd(),
		newRecordAckCmd(),
		newRecordCloseCmd(),
		newRecordReopenCmd(),
		newRecordEscalateCmd(),
		newRecordCommentCmd(),
		newRecordCommentsCmd(),
		newRecordAssignCmd(),
		newRecordReleaseCmd(),
		newRecordOwnersCmd(),
		newRecordBulkCmd(),
		newRecordAgenticCmd(),
	)
	return cmd
}

// newRecordPostCmd implements `snooze record post '<json>'`.
func newRecordPostCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "post <json>",
		Short: "Post an alert payload (JSON) to the server",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := runtimeFrom(cmd.Context())
			var rec snoozetypes.Record
			if err := json.Unmarshal([]byte(args[0]), &rec); err != nil {
				return fmt.Errorf("decode record JSON: %w", err)
			}
			c, err := rt.buildClient()
			if err != nil {
				return err
			}
			out, err := c.PostAlert(cmd.Context(), rec)
			if err != nil {
				return err
			}
			return renderRecord(cmd, rt, out)
		},
	}
}

// newRecordListCmd implements `snooze record list [filters] [--limit N]`.
// Without a filter it lists the most recent alerts; the filter flags build one
// server-side condition (see recordListFilter), so "what is firing on host X"
// is a flag, not a hand-written condition.
func newRecordListCmd() *cobra.Command {
	var (
		limit  int
		filter recordListFilter
	)
	c := &cobra.Command{
		Use:   "list",
		Short: "List recent alerts (filter by state, host, severity, owner)",
		Long: "List alerts, newest first. Filters AND together:\n" +
			"  --active              what the web Alerts tab shows: not acknowledged,\n" +
			"                        closed, shelved or snoozed\n" +
			"  --state s[,s…]        open (never touched or re-opened), ack, esc, close, shelved\n" +
			"  --host / --severity   exact match\n" +
			"  --owner me|none|login owned by you, by nobody, or by <login>\n" +
			"  -c '<json>'           any extra condition, same syntax as `snooze query`",
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt := runtimeFrom(cmd.Context())
			cl, err := rt.buildClient()
			if err != nil {
				return err
			}
			cond, err := filter.build(rt, cl)
			if err != nil {
				return err
			}
			q := ""
			if !cond.IsZero() {
				raw, err := json.Marshal(cond)
				if err != nil {
					return err
				}
				q = string(raw)
			}
			path, err := buildQueryPath("record", q, limit, 0, "date_epoch", "false")
			if err != nil {
				return err
			}
			var resp struct {
				Data []map[string]any `json:"data"`
			}
			if err := cl.Get(cmd.Context(), path, &resp); err != nil {
				return err
			}
			return renderList(cmd, rt, "record", resp.Data)
		},
	}
	c.Flags().IntVarP(&limit, "limit", "n", 50, "Maximum number of records to return")
	filter.register(c)
	return c
}

// newRecordShowCmd implements `snooze record show <uid>`. The record plugin's
// CRUD root accepts an `uid` filter via the standard `q=` base64-JSON query
// parameter; we reuse buildQueryPath to assemble the URL so the wire shape
// matches the generic `snooze query` command.
func newRecordShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <uid>",
		Short: "Show a single alert by uid",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := runtimeFrom(cmd.Context())
			cl, err := rt.buildClient()
			if err != nil {
				return err
			}
			rec, err := fetchRecordByUID(cmd.Context(), cl, args[0])
			if err != nil {
				return err
			}
			if rec == nil {
				return fmt.Errorf("no record found with uid %s", args[0])
			}
			return renderDoc(cmd, rt, rec)
		},
	}
}

// newRecordAckCmd implements `snooze record ack <uid> [-m msg]`.
func newRecordAckCmd() *cobra.Command {
	var message string
	c := &cobra.Command{
		Use:   "ack <uid>",
		Short: "Acknowledge an alert (posts a comment with type=ack)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return postRecordTransition(cmd, args[0], "ack", message, "Acked via snooze CLI")
		},
	}
	c.Flags().StringVarP(&message, "message", "m", "", "Comment message (defaults to a generic note)")
	return c
}

// newRecordCloseCmd implements `snooze record close <uid> [-m msg]`.
func newRecordCloseCmd() *cobra.Command {
	var message string
	c := &cobra.Command{
		Use:   "close <uid>",
		Short: "Close an alert (posts a comment with type=close)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return postRecordTransition(cmd, args[0], "close", message, "Closed via snooze CLI")
		},
	}
	c.Flags().StringVarP(&message, "message", "m", "", "Comment message (defaults to a generic note)")
	return c
}

// postRecordTransition POSTs a typed comment to /api/v1/comment, which the
// server interprets as a state transition on the linked record (the comment
// plugin's AfterCreate hook updates record.state). After the transition we
// re-fetch the record so the operator-facing confirmation line includes the
// host and message snippet — matching the old Python skill's output shape.
func postRecordTransition(cmd *cobra.Command, uid, ctype, message, defaultMsg string) error {
	return postRecordComment(cmd, uid, ctype, message, defaultMsg, nil)
}

// recordCommentVerbs is the past-tense verb of the confirmation line, per
// comment type.
var recordCommentVerbs = map[string]string{
	"ack":     "Acked",
	"close":   "Closed",
	"open":    "Re-opened",
	"esc":     "Re-escalated",
	"comment": "Commented on",
	"assign":  "Assigned",
	"release": "Released",
}

// postRecordComment is postRecordTransition with extra comment fields (the
// assign comment's assignee). extra never overrides the fields set here.
func postRecordComment(cmd *cobra.Command, uid, ctype, message, defaultMsg string, extra map[string]any) error {
	if uid == "" {
		return errors.New("record uid is required")
	}
	rt := runtimeFrom(cmd.Context())
	cl, err := rt.buildClient()
	if err != nil {
		return err
	}
	if message == "" {
		message = defaultMsg
	}
	name, method := "", "local"
	if rt.flags != nil {
		name = rt.flags.User
		if rt.flags.Method != "" {
			method = rt.flags.Method
		}
	}
	body := map[string]any{}
	for k, v := range extra {
		body[k] = v
	}
	body["type"] = ctype
	body["record_uid"] = uid
	body["name"] = name
	body["method"] = method
	body["message"] = message
	var resp any
	if err := cl.Post(cmd.Context(), "/api/v1/comment", body, &resp); err != nil {
		return err
	}
	rec, _ := fetchRecordByUID(cmd.Context(), cl, uid)
	host, recMsg := "", ""
	if rec != nil {
		host, _ = rec["host"].(string)
		if m, _ := rec["message"].(string); m != "" {
			recMsg = m
			if len(recMsg) > 60 {
				recMsg = recMsg[:60]
			}
		}
	}
	verb := recordCommentVerbs[ctype]
	if verb == "" {
		verb = "Updated"
	}
	line := fmt.Sprintf("%s %s (%s: %s)", verb, uid, host, recMsg)
	if owner := ownerOf(rec); owner != "" {
		line += " — owner " + owner
	}
	_, _ = fmt.Fprintln(cmd.OutOrStdout(), line)
	return nil
}

// fetchRecordByUID issues GET /api/v1/record/?q=base64(["=","uid",<uid>])&limit=1
// and returns the first document. Returns (nil, nil) when no record matches —
// callers decide whether that is an error.
func fetchRecordByUID(ctx context.Context, cl *snoozeclient.Client, uid string) (map[string]any, error) {
	cond := fmt.Sprintf(`["=","uid",%q]`, uid)
	path, err := buildQueryPath("record", cond, 1, 0, "", "")
	if err != nil {
		return nil, err
	}
	var resp struct {
		Data []map[string]any `json:"data"`
	}
	if err := cl.Get(ctx, path, &resp); err != nil {
		return nil, err
	}
	if len(resp.Data) == 0 {
		return nil, nil
	}
	return resp.Data[0], nil
}

// listCollection issues GET /api/v1/{collection}?limit=N and returns the data
// slice. It decodes into the canonical CRUD envelope shape (Data + Meta).
func listCollection(ctx context.Context, c *snoozeclient.Client, collection string, limit int) ([]map[string]any, error) {
	if collection == "" {
		return nil, errors.New("listCollection: empty collection")
	}
	path := "/api/v1/" + collection + "/"
	if limit > 0 {
		path += "?limit=" + strconv.Itoa(limit)
	}
	var resp struct {
		Data []map[string]any `json:"data"`
		Meta map[string]any   `json:"meta"`
	}
	if err := c.Get(ctx, path, &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// renderRecord prints a single Record either as JSON (when --json) or a
// compact key=value summary.
func renderRecord(cmd *cobra.Command, rt *runtime, rec snoozetypes.Record) error {
	out := cmd.OutOrStdout()
	if rt.flags != nil && rt.flags.JSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(rec)
	}
	if rec.UID == "" && rec.Host == "" && rec.Message == "" {
		_, _ = fmt.Fprintln(out, "(record posted; server returned an empty body)")
		return nil
	}
	_, _ = fmt.Fprintf(out, "uid=%s host=%s severity=%s message=%s\n",
		rec.UID, rec.Host, rec.Severity, rec.Message)
	return nil
}
