package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

// newTenantCmd builds the `snooze tenant …` subtree: create, list, delete.
func newTenantCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tenant",
		Short: "Manage tenants (platform-admin only)",
	}
	cmd.AddCommand(
		newTenantCreateCmd(),
		newTenantListCmd(),
		newTenantDeleteCmd(),
	)
	return cmd
}

// newTenantCreateCmd implements `snooze tenant create --id <slug> [--display-name <name>]`.
func newTenantCreateCmd() *cobra.Command {
	var id, displayName string
	c := &cobra.Command{
		Use:   "create",
		Short: "Create a new tenant",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if id == "" {
				return errors.New("tenant create: --id is required")
			}
			rt := runtimeFrom(cmd.Context())
			cl, err := rt.buildClient()
			if err != nil {
				return err
			}
			body := map[string]any{"id": id}
			if displayName != "" {
				body["display_name"] = displayName
			}
			var resp any
			if err := cl.Post(cmd.Context(), "/api/v1/tenant", body, &resp); err != nil {
				return err
			}
			if rt.flags != nil && rt.flags.JSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(resp)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Created tenant %s\n", id)
			return nil
		},
	}
	c.Flags().StringVar(&id, "id", "", "Tenant slug (lowercase, URL-safe) [required]")
	c.Flags().StringVar(&displayName, "display-name", "", "Human-readable name for the tenant")
	return c
}

// newTenantListCmd implements `snooze tenant list`.
func newTenantListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all tenants",
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt := runtimeFrom(cmd.Context())
			cl, err := rt.buildClient()
			if err != nil {
				return err
			}
			var resp struct {
				Data []map[string]any `json:"data"`
			}
			if err := cl.Get(cmd.Context(), "/api/v1/tenant", &resp); err != nil {
				return err
			}
			if rt.flags != nil && rt.flags.JSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(resp.Data)
			}
			// Human-readable table.
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprintln(w, "ID\tDISPLAY NAME\tSTATUS")
			for _, t := range resp.Data {
				id, _ := t["id"].(string)
				name, _ := t["display_name"].(string)
				status, _ := t["status"].(string)
				_, _ = fmt.Fprintf(w, "%s\t%s\t%s\n", id, name, status)
			}
			return w.Flush()
		},
	}
}

// newTenantDeleteCmd implements `snooze tenant delete <id>`.
func newTenantDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a tenant (irreversible; default tenant cannot be deleted)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			if id == "" {
				return errors.New("tenant delete: id argument is required")
			}
			rt := runtimeFrom(cmd.Context())
			cl, err := rt.buildClient()
			if err != nil {
				return err
			}
			var resp any
			if err := cl.Delete(cmd.Context(), "/api/v1/tenant/"+id, &resp); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Deleted tenant %s\n", id)
			return nil
		},
	}
}
