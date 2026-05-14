package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

// newSnoozeCmd builds the `snooze snooze …` subtree for snooze-filter CRUD.
// The collection name on the server is "snooze".
func newSnoozeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "snooze",
		Short: "Manage snooze-filter entries",
	}
	cmd.AddCommand(
		newSnoozeListCmd(),
		newSnoozeGetCmd(),
		newSnoozePostCmd(),
		newSnoozeDeleteCmd(),
	)
	return cmd
}

func newSnoozeListCmd() *cobra.Command {
	var limit int
	c := &cobra.Command{
		Use:   "list",
		Short: "List snooze-filter entries",
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt := runtimeFrom(cmd.Context())
			cl, err := rt.buildClient()
			if err != nil {
				return err
			}
			docs, err := listCollection(cmd.Context(), cl, "snooze", limit)
			if err != nil {
				return err
			}
			return renderList(cmd, rt, "snooze", docs)
		},
	}
	c.Flags().IntVarP(&limit, "limit", "n", 50, "Maximum number of snooze entries to return")
	return c
}

func newSnoozeGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <uid>",
		Short: "Get a single snooze entry by uid",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := runtimeFrom(cmd.Context())
			cl, err := rt.buildClient()
			if err != nil {
				return err
			}
			var doc map[string]any
			if err := cl.Get(cmd.Context(), "/api/v1/snooze/"+args[0], &doc); err != nil {
				return err
			}
			return renderDoc(cmd, rt, doc)
		},
	}
}

func newSnoozePostCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "post <json>",
		Short: "Create a new snooze-filter entry from inline JSON",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := runtimeFrom(cmd.Context())
			var body any
			if err := json.Unmarshal([]byte(args[0]), &body); err != nil {
				return fmt.Errorf("decode snooze JSON: %w", err)
			}
			cl, err := rt.buildClient()
			if err != nil {
				return err
			}
			var resp any
			if err := cl.Post(cmd.Context(), "/api/v1/snooze/", body, &resp); err != nil {
				return err
			}
			return renderAny(cmd, rt, resp)
		},
	}
}

func newSnoozeDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <uid>",
		Short: "Delete a snooze entry by uid",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := runtimeFrom(cmd.Context())
			cl, err := rt.buildClient()
			if err != nil {
				return err
			}
			var resp map[string]any
			if err := cl.Delete(cmd.Context(), "/api/v1/snooze/"+args[0], &resp); err != nil {
				return err
			}
			return renderAny(cmd, rt, resp)
		},
	}
}
