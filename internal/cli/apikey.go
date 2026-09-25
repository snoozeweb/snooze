package cli

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/snoozeweb/snooze/pkg/snoozeclient"
)

// whoAmI is the GET /api/v1/user/me body: the verified identity of the caller.
type whoAmI struct {
	Name        string   `json:"name"`
	Method      string   `json:"method"`
	Via         string   `json:"via"`
	TenantID    string   `json:"tenant_id"`
	Roles       []string `json:"roles"`
	Permissions []string `json:"permissions"`
	Key         *struct {
		UID       string `json:"uid"`
		Name      string `json:"name"`
		KeyPrefix string `json:"key_prefix"`
		ExpiresAt int64  `json:"expires_at"`
	} `json:"key"`
}

func fetchWhoAmI(ctx context.Context, cl *snoozeclient.Client) (whoAmI, error) {
	var me whoAmI
	if err := cl.Get(ctx, "/api/v1/user/me", &me); err != nil {
		return whoAmI{}, fmt.Errorf("who am I: %w", err)
	}
	return me, nil
}

// newWhoAmICmd implements `snooze whoami`: who the server sees behind the
// configured credential (session or API key).
func newWhoAmICmd() *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Show who the server authenticates you as (session or API key)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt := runtimeFrom(cmd.Context())
			cl, err := rt.buildClient()
			if err != nil {
				return err
			}
			if rt.flags != nil && rt.flags.JSON {
				var raw map[string]any
				if err := cl.Get(cmd.Context(), "/api/v1/user/me", &raw); err != nil {
					return err
				}
				return renderAny(cmd, rt, raw)
			}
			me, err := fetchWhoAmI(cmd.Context(), cl)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			line := fmt.Sprintf("%s (%s) via %s", me.Name, me.Method, me.Via)
			if k := me.Key; k != nil {
				line += fmt.Sprintf(" %q [%s…]", k.Name, k.KeyPrefix)
				if k.ExpiresAt > 0 {
					line += ", expires " + time.Unix(k.ExpiresAt, 0).UTC().Format(time.RFC3339)
				}
			}
			if me.TenantID != "" {
				line += ", tenant " + me.TenantID
			}
			_, _ = fmt.Fprintln(out, line)
			if len(me.Roles) > 0 {
				_, _ = fmt.Fprintln(out, "roles:      ", strings.Join(me.Roles, ", "))
			}
			_, _ = fmt.Fprintln(out, "permissions:", strings.Join(me.Permissions, ", "))
			return nil
		},
	}
}

// newAPIKeyCmd builds `snooze apikey {create,list,revoke}`: the caller's own
// API keys, over /api/v1/user/me/apikeys.
func newAPIKeyCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "apikey",
		Short: "Manage your personal API keys",
		Long: "A personal API key (snz_…) authenticates the CLI and scripts as you, with a\n" +
			"subset of your permissions, without a password. Put it in client.yaml as\n" +
			"credentials.token, or in $SNOOZE_TOKEN.",
	}
	c.AddCommand(newAPIKeyCreateCmd(), newAPIKeyListCmd(), newAPIKeyRevokeCmd())
	return c
}

func newAPIKeyCreateCmd() *cobra.Command {
	var (
		name    string
		perms   []string
		expires string
	)
	c := &cobra.Command{
		Use:   "create --name NAME --perm PERM [--perm PERM ...] [--expires 90d|RFC3339]",
		Short: "Mint an API key (the key is shown once)",
		Long: "Mint a personal API key carrying a subset of your own permissions. The server\n" +
			"refuses to mint while the request is itself authenticated with an API key, so\n" +
			"run this from a password session.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt := runtimeFrom(cmd.Context())
			if strings.TrimSpace(name) == "" {
				return errors.New("--name is required")
			}
			if rt.flags != nil && snoozeclient.IsAPIKey(rt.flags.Token) {
				return errors.New("you are authenticated with an API key, and a key cannot mint another; " +
					"use a password session instead: snooze --token= --user <login> apikey create …")
			}
			var expiresAt string
			if expires != "" {
				t, err := parseExpiry(expires, time.Now())
				if err != nil {
					return err
				}
				expiresAt = t.UTC().Format(time.RFC3339)
			}
			cl, err := rt.buildClient()
			if err != nil {
				return err
			}
			if len(perms) == 0 {
				// Never default to "everything": make the caller choose.
				me, err := fetchWhoAmI(cmd.Context(), cl)
				if err != nil {
					return errors.New("--perm is required (at least one permission to grant)")
				}
				return fmt.Errorf("--perm is required: pick the permissions to grant from your own: %s",
					strings.Join(me.Permissions, ", "))
			}
			body := map[string]any{"name": name, "permissions": perms}
			if expiresAt != "" {
				body["expires_at"] = expiresAt
			}
			var created map[string]any
			if err := cl.Post(cmd.Context(), "/api/v1/user/me/apikeys", body, &created); err != nil {
				return err
			}
			if rt.flags != nil && rt.flags.JSON {
				return renderAny(cmd, rt, created)
			}
			key, _ := created["key"].(string)
			uid, _ := created["uid"].(string)
			out := cmd.OutOrStdout()
			_, _ = fmt.Fprintf(out, "Created API key %q (uid %s, expires %s).\n", name, uid, epochString(created["expires_at"]))
			_, _ = fmt.Fprintf(out, "\nKey — shown once, copy it now:\n\n  %s\n\n", key)
			_, _ = fmt.Fprintf(out, "Use it from ~/.config/snooze/client.yaml (then chmod 600 it):\n\n"+
				"  credentials:\n    token: %s\n\nor: export SNOOZE_TOKEN=%s\n", key, key)
			return nil
		},
	}
	c.Flags().StringVar(&name, "name", "", "Key name (unique per user)")
	c.Flags().StringSliceVar(&perms, "perm", nil, "Permission to grant (repeat or comma-separate); must be one you hold")
	c.Flags().StringVar(&expires, "expires", "", "Expiry: a duration (90d, 720h) or an RFC3339 time; default the server's cap")
	return c
}

func newAPIKeyListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List your API keys (secrets are never shown)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt := runtimeFrom(cmd.Context())
			cl, err := rt.buildClient()
			if err != nil {
				return err
			}
			var resp struct {
				Data []map[string]any `json:"data"`
			}
			if err := cl.Get(cmd.Context(), "/api/v1/user/me/apikeys", &resp); err != nil {
				return err
			}
			if rt.flags != nil && rt.flags.JSON {
				return renderAny(cmd, rt, resp.Data)
			}
			if len(resp.Data) == 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "(no API keys)")
				return nil
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			defer tw.Flush() //nolint:errcheck
			_, _ = fmt.Fprintln(tw, "uid\tname\tprefix\tpermissions\texpires\tlast_used")
			for _, k := range resp.Data {
				_, _ = fmt.Fprintf(tw, "%s\t%s\t%s…\t%s\t%s\t%s\n",
					str(k["uid"]), str(k["name"]), str(k["key_prefix"]),
					strings.Join(anyStrings(k["permissions"]), ","),
					epochString(k["expires_at"]), epochString(k["last_used_at"]))
			}
			return nil
		},
	}
}

func newAPIKeyRevokeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "revoke <uid>",
		Short: "Revoke one of your API keys",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := runtimeFrom(cmd.Context())
			cl, err := rt.buildClient()
			if err != nil {
				return err
			}
			if err := cl.Delete(cmd.Context(), "/api/v1/user/me/apikeys/"+url.PathEscape(args[0]), nil); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Revoked API key %s\n", args[0])
			return nil
		},
	}
}

// parseExpiry accepts an RFC3339 time, a Go duration ("720h"), or a whole
// number of days ("90d"), the last two relative to now.
func parseExpiry(s string, now time.Time) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	if days, ok := strings.CutSuffix(s, "d"); ok {
		if n, err := strconv.Atoi(days); err == nil && n > 0 {
			return now.Add(time.Duration(n) * 24 * time.Hour), nil
		}
	}
	if d, err := time.ParseDuration(s); err == nil && d > 0 {
		return now.Add(d), nil
	}
	return time.Time{}, fmt.Errorf("--expires %q: want a duration (90d, 720h) or an RFC3339 time", s)
}

// epochString renders a stored epoch-seconds value as RFC3339, "never" when
// absent or zero.
func epochString(v any) string {
	var n int64
	switch x := v.(type) {
	case float64:
		n = int64(x)
	case int64:
		n = x
	case int:
		n = int64(x)
	}
	if n <= 0 {
		return "never"
	}
	return time.Unix(n, 0).UTC().Format(time.RFC3339)
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func anyStrings(v any) []string {
	items, _ := v.([]any)
	out := make([]string, 0, len(items))
	for _, it := range items {
		if s, ok := it.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
