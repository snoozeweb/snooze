package cli

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

// renderList prints a list of documents either as JSON (rt.flags.JSON) or a
// per-collection tabular view. We cherry-pick the most useful columns for
// each collection so the default output stays readable.
func renderList(cmd *cobra.Command, rt *runtime, collection string, docs []map[string]any) error {
	out := cmd.OutOrStdout()
	if rt.flags != nil && rt.flags.JSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(docs)
	}
	if len(docs) == 0 {
		fmt.Fprintln(out, "(no records)")
		return nil
	}
	cols := columnsFor(collection, docs)
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	defer tw.Flush()
	fmt.Fprintln(tw, strings.Join(cols, "\t"))
	for _, d := range docs {
		row := make([]string, 0, len(cols))
		for _, c := range cols {
			row = append(row, fmt.Sprintf("%v", coerce(d[c])))
		}
		fmt.Fprintln(tw, strings.Join(row, "\t"))
	}
	return nil
}

// renderDoc prints a single document.
func renderDoc(cmd *cobra.Command, rt *runtime, doc map[string]any) error {
	out := cmd.OutOrStdout()
	if rt.flags != nil && rt.flags.JSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(doc)
	}
	keys := make([]string, 0, len(doc))
	for k := range doc {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(out, "%s: %v\n", k, coerce(doc[k]))
	}
	return nil
}

// renderAny prints whatever the server returned: a JSON dump unless the value
// is a flat map, in which case we use renderDoc.
func renderAny(cmd *cobra.Command, rt *runtime, v any) error {
	out := cmd.OutOrStdout()
	if rt.flags != nil && rt.flags.JSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(v)
	}
	if m, ok := v.(map[string]any); ok {
		return renderDoc(cmd, rt, m)
	}
	// Fall back to JSON for non-map results.
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// columnsFor picks the columns shown in the default table view. The list is
// per-collection because each plugin's documents have a different shape;
// unrecognised collections fall back to the union of top-level keys.
func columnsFor(collection string, docs []map[string]any) []string {
	switch collection {
	case "record":
		return []string{"uid", "host", "severity", "state", "message"}
	case "snooze":
		return []string{"uid", "name", "ql", "ttl"}
	}
	keys := map[string]struct{}{}
	for _, d := range docs {
		for k := range d {
			if k == "_id" {
				continue
			}
			keys[k] = struct{}{}
		}
	}
	out := make([]string, 0, len(keys))
	for k := range keys {
		out = append(out, k)
	}
	sort.Strings(out)
	// Cap at 6 columns to keep the table readable.
	if len(out) > 6 {
		out = out[:6]
	}
	return out
}

// coerce flattens common nested values to a printable form. Strings are
// returned as-is; slices and maps are JSON-encoded so the output stays on
// one line.
func coerce(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool, float64, int, int64, uint64:
		return fmt.Sprintf("%v", x)
	default:
		raw, err := json.Marshal(x)
		if err != nil {
			return fmt.Sprintf("%v", x)
		}
		return string(raw)
	}
}
