package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

// newTenantCmd builds the `snooze tenant …` subtree: create, list, delete, reset-admin.
func newTenantCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tenant",
		Short: "Manage tenants (platform-admin only)",
	}
	cmd.AddCommand(
		newTenantCreateCmd(),
		newTenantListCmd(),
		newTenantDeleteCmd(),
		newTenantResetAdminCmd(),
	)
	return cmd
}

// newTenantCreateCmd implements `snooze tenant create --id <slug> [--display-name <name>] [--no-admin] [--admin-username <name>]`.
func newTenantCreateCmd() *cobra.Command {
	var id, displayName, adminUsername string
	var noAdmin bool
	c := &cobra.Command{
		Use:   "create",
		Short: "Create a new tenant",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if id == "" {
				return errors.New("tenant create: --id is required")
			}
			if noAdmin && adminUsername != "" {
				return errors.New("tenant create: --admin-username cannot be used with --no-admin")
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
			if noAdmin {
				body["create_admin"] = false
			}
			if adminUsername != "" {
				body["admin_username"] = adminUsername
			}
			var resp struct {
				Added []string `json:"added"`
				Admin *struct {
					Username string `json:"username"`
					Password string `json:"password"`
				} `json:"admin"`
			}
			if err := cl.Post(cmd.Context(), "/api/v1/tenant", body, &resp); err != nil {
				return err
			}
			if rt.flags != nil && rt.flags.JSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(resp)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Created tenant %s\n", id)
			if resp.Admin != nil {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(),
					"Admin user: %s (local)\nPassword:   %s   — shown once; store it now.\n",
					resp.Admin.Username, resp.Admin.Password)
			}
			return nil
		},
	}
	c.Flags().StringVar(&id, "id", "", "Tenant slug (lowercase, URL-safe) [required]")
	c.Flags().StringVar(&displayName, "display-name", "", "Human-readable name for the tenant")
	c.Flags().BoolVar(&noAdmin, "no-admin", false, "Do not create a first admin user (e.g. LDAP/SSO-only tenants)")
	c.Flags().StringVar(&adminUsername, "admin-username", "", "Username for the first admin user (default \"admin\")")
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

// newTenantResetAdminCmd implements `snooze tenant reset-admin --id <id> [--admin-username <name>]`.
func newTenantResetAdminCmd() *cobra.Command {
	var id, adminUsername string
	c := &cobra.Command{
		Use:   "reset-admin",
		Short: "Generate a new password for a tenant's local admin (creates it if absent)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if id == "" {
				return errors.New("tenant reset-admin: --id is required")
			}
			rt := runtimeFrom(cmd.Context())
			cl, err := rt.buildClient()
			if err != nil {
				return err
			}
			body := map[string]any{}
			if adminUsername != "" {
				body["username"] = adminUsername
			}
			var resp struct {
				Username string `json:"username"`
				Password string `json:"password"`
			}
			if err := cl.Post(cmd.Context(), "/api/v1/tenant/"+id+"/admin", body, &resp); err != nil {
				return err
			}
			if rt.flags != nil && rt.flags.JSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(resp) //nolint:gosec // intentional: printing the generated password to stdout for the operator to copy
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(),
				"Admin user: %s (local)\nPassword:   %s   — shown once; store it now.\n",
				resp.Username, resp.Password)
			return nil
		},
	}
	c.Flags().StringVar(&id, "id", "", "Tenant slug [required]")
	c.Flags().StringVar(&adminUsername, "admin-username", "", "Admin username to reset (default \"admin\")")
	return c
}
