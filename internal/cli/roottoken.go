package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"

	"github.com/spf13/cobra"
)

// defaultAdminSocket is the canonical path where snooze-server binds its
// privileged admin socket. The socket's filesystem permissions plus the
// SO_PEERCRED check on the server side enforce that only the daemon's uid
// (or root) can read the root token.
const defaultAdminSocket = "/var/run/snooze/admin.sock"

// newRootTokenCmd implements `snooze root-token`. It dials the unix admin
// socket, requests /api/root_token, and prints the issued token. The socket
// path is overridable via --socket; tests inject their own httptest URL via
// the runtime's httpClient.
func newRootTokenCmd() *cobra.Command {
	var socketPath string
	cmd := &cobra.Command{
		Use:   "root-token",
		Short: "Fetch a one-shot root token from the admin unix socket",
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt := runtimeFrom(cmd.Context())
			path := socketPath
			if path == "" {
				path = defaultAdminSocket
			}
			tok, err := fetchRootToken(cmd.Context(), rt, path)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if rt.flags != nil && rt.flags.JSON {
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(map[string]string{"root_token": tok})
			}
			_, _ = fmt.Fprintln(out, tok)
			return nil
		},
	}
	cmd.Flags().StringVarP(&socketPath, "socket", "s", defaultAdminSocket,
		"Path to the snooze-server admin unix socket")
	return cmd
}

// fetchRootToken dials the unix socket at path and issues GET /api/root_token.
// When rt.httpClient is non-nil (tests), it is used as-is — the URL passed in
// the request is interpreted by the injected transport.
func fetchRootToken(ctx context.Context, rt *runtime, path string) (string, error) {
	hc := rt.httpClient
	url := "http://admin/api/root_token"
	if hc == nil {
		// Production path: dial the unix socket directly. A custom transport
		// rewrites Dial to point at the socket regardless of the URL host.
		hc = &http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					var d net.Dialer
					return d.DialContext(ctx, "unix", path)
				},
			},
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return "", fmt.Errorf("admin socket %s: %w", path, err)
	}
	defer resp.Body.Close() //nolint:errcheck
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("admin socket %s: status %d: %s", path, resp.StatusCode, string(body))
	}
	var payload struct {
		RootToken string `json:"root_token"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("decode admin response: %w", err)
	}
	if payload.RootToken == "" {
		return "", fmt.Errorf("admin socket %s: empty root_token in response", path)
	}
	return payload.RootToken, nil
}
