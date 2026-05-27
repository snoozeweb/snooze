package newrelic

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestNewRelicE2E posts a realistic workflow webhook body to a running
// snooze-server and asserts a 2xx response.
//
// Environment variables:
//
//	SNOOZE_E2E_NEWRELIC_URL — full URL of the running snooze-server's New Relic
//	                          webhook endpoint, e.g.
//	                          https://snooze.example.com/api/v1/webhook/newrelic
//
// The test is skipped when SNOOZE_E2E_NEWRELIC_URL is unset.
func TestNewRelicE2E(t *testing.T) {
	targetURL := os.Getenv("SNOOZE_E2E_NEWRELIC_URL")
	if targetURL == "" {
		t.Skip("set SNOOZE_E2E_NEWRELIC_URL to run the New Relic end-to-end test")
	}

	body := map[string]any{
		"id":               "e2e-test-issue-001",
		"issueUrl":         "https://one.newrelic.com/alerts-ai/issues/e2e-test-issue-001",
		"title":            "E2E test alert from snooze integration test",
		"priority":         "LOW",
		"state":            "ACTIVATED",
		"trigger":          "INCIDENT_ADDED",
		"timestamp":        1716800000000,
		"accountName":      "Snooze E2E",
		"totalIncidents":   1,
		"owner":            "e2e-runner",
		"impactedEntities": []string{"snooze-e2e-entity"},
		"labels": map[string]string{
			"env":  "test",
			"team": "platform",
		},
	}

	payload, err := json.Marshal(body)
	require.NoError(t, err)

	resp, err := http.Post(targetURL, "application/json", bytes.NewReader(payload)) //nolint:noctx
	require.NoError(t, err)
	defer resp.Body.Close()

	require.True(t,
		resp.StatusCode >= 200 && resp.StatusCode < 300,
		"expected 2xx from snooze-server, got %d", resp.StatusCode,
	)
}
