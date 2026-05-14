package cli

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/spf13/cobra"
)

// newHealthCmd implements `snooze health`. It hits /healthz and /readyz
// independently so an operator can spot a half-up server (process alive, db
// unreachable).
func newHealthCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "health",
		Short: "Check the server's liveness and readiness probes",
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt := runtimeFrom(cmd.Context())
			if rt.flags == nil || rt.flags.Server == "" {
				return fmt.Errorf("--server is required")
			}
			hc := rt.httpClient
			if hc == nil {
				hc = healthHTTPClient(rt.flags.Insecure)
			}
			base := strings.TrimRight(rt.flags.Server, "/")
			results := []probeResult{
				probe(cmd.Context(), hc, base+"/healthz"),
				probe(cmd.Context(), hc, base+"/readyz"),
			}
			out := cmd.OutOrStdout()
			if rt.flags.JSON {
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				if err := enc.Encode(results); err != nil {
					return err
				}
			} else {
				for _, r := range results {
					fmt.Fprintf(out, "%-12s %-3d %s\n", r.Endpoint, r.Status, r.Body)
				}
			}
			for _, r := range results {
				if r.Status < 200 || r.Status >= 300 {
					return fmt.Errorf("health check %s failed (status %d)", r.Endpoint, r.Status)
				}
			}
			return nil
		},
	}
}

// healthHTTPClient builds a standalone *http.Client for the health probes.
// We don't reuse snoozeclient.Client because /healthz lives at the root and
// has no auth or retry semantics worth its weight.
func healthHTTPClient(insecure bool) *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if insecure {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // opt-in
	}
	return &http.Client{Transport: tr}
}

// probeResult is a one-shot health probe output line.
type probeResult struct {
	Endpoint string `json:"endpoint"`
	Status   int    `json:"status"`
	Body     string `json:"body,omitempty"`
}

// probe issues GET url and captures the status + truncated body. Network
// errors land in Body as a plain string and Status remains 0.
func probe(ctx context.Context, hc *http.Client, url string) probeResult {
	// Extract the path portion so the output is stable across hosts.
	endpoint := url
	if idx := strings.LastIndex(url, "/"); idx > 0 && idx < len(url)-1 {
		endpoint = url[idx:]
	}
	res := probeResult{Endpoint: endpoint}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		res.Body = err.Error()
		return res
	}
	req.Header.Set("Accept", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		res.Body = err.Error()
		return res
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	res.Status = resp.StatusCode
	res.Body = strings.TrimSpace(string(body))
	return res
}
