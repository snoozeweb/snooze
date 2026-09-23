package cli

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

// newRecordAssignCmd implements `snooze record assign <uid> <user>
// [--user-method m] [-m msg]`: an `assign` comment makes <user> the alert's
// owner without changing its state. The server rejects a closed alert and an
// unknown or disabled user.
func newRecordAssignCmd() *cobra.Command {
	var message, userMethod string
	c := &cobra.Command{
		Use:   "assign <uid> <user>",
		Short: "Make <user> the owner of an alert (posts a comment with type=assign)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			extra := map[string]any{"assignee": args[1]}
			if userMethod != "" {
				extra["assignee_method"] = userMethod
			}
			return postRecordComment(cmd, args[0], "assign", message,
				"Assigned to "+args[1]+" via snooze CLI", extra)
		},
	}
	c.Flags().StringVarP(&message, "message", "m", "", "Comment message (defaults to a generic note)")
	c.Flags().StringVar(&userMethod, "user-method", "",
		"Auth method of <user> (local, ldap, oidc, …); only needed when the login exists for several methods")
	return c
}

// newRecordReleaseCmd implements `snooze record release <uid> [-m msg]`: a
// `release` comment clears the owner (kept as the previous owner); releasing
// an acknowledged alert also returns it to open.
func newRecordReleaseCmd() *cobra.Command {
	var message string
	c := &cobra.Command{
		Use:   "release <uid>",
		Short: "Give an alert back: clear its owner (posts a comment with type=release)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return postRecordComment(cmd, args[0], "release", message, "Released via snooze CLI", nil)
		},
	}
	c.Flags().StringVarP(&message, "message", "m", "", "Comment message (defaults to a generic note)")
	return c
}

// ownerCount is one row of GET /api/v1/record/owners.
type ownerCount struct {
	Owner string `json:"owner"`
	Count int    `json:"count"`
}

// ownersResponse is the GET /api/v1/record/owners body.
type ownersResponse struct {
	Data    []ownerCount `json:"data"`
	Unowned int          `json:"unowned"`
	Total   int          `json:"total"`
}

// newRecordOwnersCmd implements `snooze record owners [-c cond]`: how many of
// the alerts matching the condition each owner has, plus the unowned ones.
func newRecordOwnersCmd() *cobra.Command {
	var condition string
	c := &cobra.Command{
		Use:   "owners",
		Short: "Count alerts per owner",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := checkCondition(condition); err != nil {
				return err
			}
			rt := runtimeFrom(cmd.Context())
			cl, err := rt.buildClient()
			if err != nil {
				return err
			}
			var resp ownersResponse
			if err := cl.Get(cmd.Context(), withQueryCond("/api/v1/record/owners", condition), &resp); err != nil {
				return err
			}
			if rt.flags != nil && rt.flags.JSON {
				return renderAny(cmd, rt, resp)
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			defer tw.Flush() //nolint:errcheck
			_, _ = fmt.Fprintln(tw, "owner\tcount")
			for _, o := range resp.Data {
				_, _ = fmt.Fprintf(tw, "%s\t%d\n", o.Owner, o.Count)
			}
			_, _ = fmt.Fprintf(tw, "(unowned)\t%d\n", resp.Unowned)
			_, _ = fmt.Fprintf(tw, "(total)\t%d\n", resp.Total)
			return nil
		},
	}
	c.Flags().StringVarP(&condition, "condition", "c", "",
		"JSON condition selecting the alerts to count (default: all)")
	return c
}

// bulkStates are the states POST /api/v1/record/bulk_state accepts.
var bulkStates = map[string]bool{"ack": true, "close": true, "open": true, "esc": true}

// newRecordBulkCmd builds `snooze record bulk {state,assign,release}`: one
// server call applying a change to every alert matching a condition. There is
// no implicit "everything": the target is either -c <condition> or an explicit
// --all, so a forgotten flag cannot rewrite every alert of the tenant.
func newRecordBulkCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bulk",
		Short: "Change every alert matching a condition in one call",
		Long: "Apply a state change or an ownership change to every alert matching\n" +
			"a condition (-c '<json>') or, explicitly, to all alerts (--all).\n" +
			"No per-alert comment is written; --message goes to the audit trail.",
	}
	cmd.AddCommand(newBulkStateCmd(), newBulkAssignCmd(), newBulkReleaseCmd())
	return cmd
}

// bulkTarget holds the flags every bulk subcommand shares.
type bulkTarget struct {
	condition string
	all       bool
	message   string
}

func (b *bulkTarget) register(c *cobra.Command) {
	c.Flags().StringVarP(&b.condition, "condition", "c", "", "JSON condition selecting the alerts")
	c.Flags().BoolVar(&b.all, "all", false, "Target every alert of the tenant (instead of -c)")
	c.Flags().StringVarP(&b.message, "message", "m", "", "Note recorded once in the audit summary")
}

// path returns the bulk endpoint URL with the target condition, refusing an
// ambiguous or missing target.
func (b *bulkTarget) path(endpoint string) (string, error) {
	switch {
	case b.condition != "" && b.all:
		return "", errors.New("use either --condition or --all, not both")
	case b.condition == "" && !b.all:
		return "", errors.New("select the alerts with --condition '<json>', or pass --all to target every alert")
	}
	if err := checkCondition(b.condition); err != nil {
		return "", err
	}
	return withQueryCond(endpoint, b.condition), nil
}

// bulkResult is the common shape of the bulk endpoints' responses; State is
// set by bulk_state, Action by bulk_owner.
type bulkResult struct {
	Matched int    `json:"matched"`
	Updated int    `json:"updated"`
	State   string `json:"state,omitempty"`
	Action  string `json:"action,omitempty"`
}

// postBulk sends one bulk request and prints its counts.
func postBulk(cmd *cobra.Command, target *bulkTarget, endpoint string, body map[string]any) error {
	path, err := target.path(endpoint)
	if err != nil {
		return err
	}
	rt := runtimeFrom(cmd.Context())
	cl, err := rt.buildClient()
	if err != nil {
		return err
	}
	if target.message != "" {
		body["message"] = target.message
	}
	var resp bulkResult
	if err := cl.Post(cmd.Context(), path, body, &resp); err != nil {
		return err
	}
	if rt.flags != nil && rt.flags.JSON {
		return renderAny(cmd, rt, resp)
	}
	what := resp.State
	if what == "" {
		what = resp.Action
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s: %d matched, %d updated\n", what, resp.Matched, resp.Updated)
	return nil
}

func newBulkStateCmd() *cobra.Command {
	var target bulkTarget
	c := &cobra.Command{
		Use:   "state <ack|close|open|esc>",
		Short: "Set the state of every matching alert (ack/close also take ownership)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !bulkStates[args[0]] {
				return fmt.Errorf("state must be one of ack, close, open, esc (got %q)", args[0])
			}
			return postBulk(cmd, &target, "/api/v1/record/bulk_state", map[string]any{"state": args[0]})
		},
	}
	target.register(c)
	return c
}

func newBulkAssignCmd() *cobra.Command {
	var (
		target     bulkTarget
		userMethod string
	)
	c := &cobra.Command{
		Use:   "assign <user>",
		Short: "Make <user> the owner of every matching alert (closed ones are skipped)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body := map[string]any{"action": "assign", "assignee": args[0]}
			if userMethod != "" {
				body["assignee_method"] = userMethod
			}
			return postBulk(cmd, &target, "/api/v1/record/bulk_owner", body)
		},
	}
	target.register(c)
	c.Flags().StringVar(&userMethod, "user-method", "",
		"Auth method of <user>; only needed when the login exists for several methods")
	return c
}

func newBulkReleaseCmd() *cobra.Command {
	var target bulkTarget
	c := &cobra.Command{
		Use:   "release",
		Short: "Clear the owner of every matching alert (acknowledged ones return to open)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return postBulk(cmd, &target, "/api/v1/record/bulk_owner", map[string]any{"action": "release"})
		},
	}
	target.register(c)
	return c
}

// withQueryCond appends a JSON condition to path as the base64url `q`
// parameter the query-wide endpoints decode (the same encoding as the CRUD
// list `q`, see buildQueryPath). An empty condition leaves path untouched.
func withQueryCond(path, condition string) string {
	condition = strings.TrimSpace(condition)
	if condition == "" {
		return path
	}
	return path + "?" + url.Values{"q": {base64.RawURLEncoding.EncodeToString([]byte(condition))}}.Encode()
}

// ownerOf reads the owner off a record document for a confirmation line. The
// value is a plain string; anything else (absent, cleared) reads as unowned.
func ownerOf(rec map[string]any) string {
	if rec == nil {
		return ""
	}
	owner, _ := rec["owner"].(string)
	return owner
}

// checkCondition rejects a --condition that is not JSON before anything is
// sent: on a bulk write a typo must fail here, not as a server-side 400 after
// the operator assumed it went through.
func checkCondition(condition string) error {
	if strings.TrimSpace(condition) == "" {
		return nil
	}
	var v any
	if err := json.Unmarshal([]byte(condition), &v); err != nil {
		return fmt.Errorf("invalid --condition JSON: %w", err)
	}
	return nil
}
