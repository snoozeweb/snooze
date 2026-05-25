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
// record, and `ack` / `close` to drive state transitions via the comment
// endpoint.
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

// newRecordListCmd implements `snooze record list [--limit N]`.
func newRecordListCmd() *cobra.Command {
	var limit int
	c := &cobra.Command{
		Use:   "list",
		Short: "List recent alerts",
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt := runtimeFrom(cmd.Context())
			cl, err := rt.buildClient()
			if err != nil {
				return err
			}
			docs, err := listCollection(cmd.Context(), cl, "record", limit)
			if err != nil {
				return err
			}
			return renderList(cmd, rt, "record", docs)
		},
	}
	c.Flags().IntVarP(&limit, "limit", "n", 50, "Maximum number of records to return")
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
	body := map[string]any{
		"type":       ctype,
		"record_uid": uid,
		"name":       name,
		"method":     method,
		"message":    message,
	}
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
	verb := "Acked"
	if ctype == "close" {
		verb = "Closed"
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s %s (%s: %s)\n", verb, uid, host, recMsg)
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
