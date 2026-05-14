package cli

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

// newQueryCmd implements `snooze query <plugin> [--condition '...']`. It is the
// generic CRUD GET that worked against any plugin's collection in the Python
// CLI. The --condition flag is forwarded as the base64-url-encoded `q` param,
// matching the contract of internal/plugins/crud.go decodeListParams.
func newQueryCmd() *cobra.Command {
	var (
		condition string
		limit     int
		offset    int
		orderBy   string
		ascFlag   string
	)
	c := &cobra.Command{
		Use:   "query <plugin>",
		Short: "Run a generic list query against any plugin collection",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := runtimeFrom(cmd.Context())
			plugin := strings.TrimSpace(args[0])
			if plugin == "" {
				return errors.New("plugin name is required")
			}
			cl, err := rt.buildClient()
			if err != nil {
				return err
			}
			path, err := buildQueryPath(plugin, condition, limit, offset, orderBy, ascFlag)
			if err != nil {
				return err
			}
			var resp struct {
				Data []map[string]any `json:"data"`
				Meta map[string]any   `json:"meta"`
			}
			if err := cl.Get(cmd.Context(), path, &resp); err != nil {
				return err
			}
			return renderList(cmd, rt, plugin, resp.Data)
		},
	}
	c.Flags().StringVarP(&condition, "condition", "c", "",
		"JSON condition to filter results, forwarded as ?q= (base64url-encoded)")
	c.Flags().IntVarP(&limit, "limit", "n", 0, "Max records (0 = server default)")
	c.Flags().IntVar(&offset, "offset", 0, "Result offset")
	c.Flags().StringVar(&orderBy, "orderby", "", "Field to order by")
	c.Flags().StringVar(&ascFlag, "asc", "", "true/false sort direction")
	return c
}

// buildQueryPath assembles the CRUD list URL with the canonical query params.
// condition is a JSON snippet that we re-encode as base64-url so the server's
// decodeListParams can round-trip it.
func buildQueryPath(plugin, condition string, limit, offset int, orderBy, ascFlag string) (string, error) {
	q := url.Values{}
	if condition != "" {
		encoded := base64.RawURLEncoding.EncodeToString([]byte(condition))
		q.Set("q", encoded)
	}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	if offset > 0 {
		q.Set("offset", strconv.Itoa(offset))
	}
	if orderBy != "" {
		q.Set("orderby", orderBy)
	}
	if ascFlag != "" {
		// Validate boolean so we surface a nicer error than the server would.
		if _, err := strconv.ParseBool(ascFlag); err != nil {
			return "", fmt.Errorf("invalid --asc %q: %w", ascFlag, err)
		}
		q.Set("asc", ascFlag)
	}
	path := "/api/v1/" + plugin + "/"
	if encoded := q.Encode(); encoded != "" {
		path += "?" + encoded
	}
	return path, nil
}
