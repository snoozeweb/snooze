package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/spf13/cobra"

	"github.com/snoozeweb/snooze/pkg/snoozeclient"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// newRecordAgenticCmd builds `snooze record agentic {get,set,clear} <uid>`:
// the operator-facing surface of an alert's protected `agentic` subtree (the
// AI-authored root cause + remediation plan).
//
// Writes go to PUT /api/v1/record/{uid}/agentic, the only path that accepts
// them — the generic record endpoints refuse the field outright. The caller
// needs the literal `rw_protected` permission; a plain rw_all admin token
// gets a 403, which is the intended behaviour, not a misconfiguration.
func newRecordAgenticCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agentic",
		Short: "Read, write or clear an alert's agentic analysis",
		Long: "Manage the protected `agentic` subtree on an alert: the AI-authored\n" +
			"root cause and remediation plan. Writing requires the rw_protected permission.",
	}
	cmd.AddCommand(newAgenticGetCmd(), newAgenticSetCmd(), newAgenticClearCmd())
	return cmd
}

func newAgenticGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <uid>",
		Short: "Show the agentic analysis stored on an alert",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := runtimeFrom(cmd.Context())
			cl, err := rt.buildClient()
			if err != nil {
				return err
			}
			var resp agenticEnvelope
			if err := cl.Get(cmd.Context(), agenticPath(args[0]), &resp); err != nil {
				return renderAgenticError(cmd, err)
			}
			return renderAny(cmd, rt, resp.Agentic)
		},
	}
}

func newAgenticSetCmd() *cobra.Command {
	var source, file string
	c := &cobra.Command{
		Use:   "set <uid> [json]",
		Short: "Store the agentic analysis on an alert (replaces any existing one)",
		Long: "Store an analysis. The payload is\n" +
			`  {"root_cause": {"summary": …, "scope": …, "evidence": […], "confidence": "high|medium|low"},` + "\n" +
			`   "remediation_plan": {"steps": [{"action": …, "command": …, "risk": "low|medium|high"}],` + "\n" +
			`                        "rollback": [ …same shape… ], "automatable": true|false}}` + "\n\n" +
			"Pass it as an argument, or with --file (- for stdin). The `analysis`\n" +
			"provenance block is stamped by the server and must not be supplied.",
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := agenticPayload(cmd, args, file)
			if err != nil {
				return err
			}
			// Validate locally first: the same schema the server enforces, so
			// an agent gets the field-level errors without a round-trip (and
			// without spending a write permission on a doomed request).
			req, err := snoozetypes.DecodeAgenticRequest(raw)
			if err != nil {
				var verrs snoozetypes.ValidationErrors
				if errors.As(err, &verrs) {
					printFieldErrors(cmd, verrs.Details())
					return errors.New("agentic payload failed schema validation")
				}
				return fmt.Errorf("decode agentic JSON: %w", err)
			}
			if req.Source == "" {
				req.Source = source
			}

			rt := runtimeFrom(cmd.Context())
			cl, err := rt.buildClient()
			if err != nil {
				return err
			}
			var resp agenticEnvelope
			if err := cl.Put(cmd.Context(), agenticPath(args[0]), req, &resp); err != nil {
				return renderAgenticError(cmd, err)
			}
			if rt.flags != nil && rt.flags.JSON {
				return renderAny(cmd, rt, resp.Agentic)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Stored agentic analysis on %s (confidence %s, %d step(s))\n",
				args[0], req.RootCause.Confidence, len(req.RemediationPlan.Steps))
			return nil
		},
	}
	c.Flags().StringVar(&source, "source", "snooze-cli",
		"Tool tag recorded in analysis.source (ignored when the payload sets its own)")
	c.Flags().StringVarP(&file, "file", "f", "",
		"Read the JSON payload from a file, or - for stdin")
	return c
}

func newAgenticClearCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "clear <uid>",
		Short: "Remove the agentic analysis from an alert",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := runtimeFrom(cmd.Context())
			cl, err := rt.buildClient()
			if err != nil {
				return err
			}
			if err := cl.Delete(cmd.Context(), agenticPath(args[0]), nil); err != nil {
				return renderAgenticError(cmd, err)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Cleared agentic analysis on %s\n", args[0])
			return nil
		},
	}
}

// agenticEnvelope is the {uid, agentic} body GET and PUT return.
type agenticEnvelope struct {
	UID     string         `json:"uid"`
	Agentic map[string]any `json:"agentic"`
}

func agenticPath(uid string) string { return "/api/v1/record/" + uid + "/agentic" }

// agenticPayload resolves the JSON body from --file (- for stdin) or the
// positional argument, insisting on exactly one of the two.
func agenticPayload(cmd *cobra.Command, args []string, file string) ([]byte, error) {
	hasArg := len(args) > 1
	switch {
	case file != "" && hasArg:
		return nil, errors.New("pass the payload either as an argument or with --file, not both")
	case file == "-":
		return io.ReadAll(cmd.InOrStdin())
	case file != "":
		return os.ReadFile(file) //nolint:gosec // operator-supplied path, by design
	case hasArg:
		return []byte(args[1]), nil
	}
	return nil, errors.New("no payload: pass JSON as an argument or use --file")
}

// renderAgenticError prints a server-side validation failure as one line per
// offending field before returning the error, so the caller sees what to fix
// rather than a bare 422.
func renderAgenticError(cmd *cobra.Command, err error) error {
	apiErr, ok := snoozeclient.IsAPIError(err)
	if !ok || len(apiErr.Details) == 0 {
		return err
	}
	printFieldErrors(cmd, apiErr.Details)
	return err
}

func printFieldErrors(cmd *cobra.Command, details map[string]any) {
	paths := make([]string, 0, len(details))
	for p := range details {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	out := cmd.ErrOrStderr()
	for _, p := range paths {
		_, _ = fmt.Fprintf(out, "  %s: %v\n", p, details[p])
	}
}
