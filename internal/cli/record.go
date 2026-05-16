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
// an alert and `list` to fetch recent records.
func newRecordCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "record",
		Short: "Manage records (alerts)",
	}
	cmd.AddCommand(newRecordPostCmd(), newRecordListCmd())
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
