package datadog

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestDatadogE2E sends a realistic Datadog monitor webhook to a running
// snooze-server instance and asserts a 2xx response.
//
// Skip conditions: SNOOZE_E2E_DATADOG_URL is unset (default in CI).
//
// To run manually:
//
//	export SNOOZE_E2E_DATADOG_URL="http://localhost:5200/api/v1/webhook/datadog"
//	go test -run TestDatadogE2E ./internal/pluginimpl/datadog/...
func TestDatadogE2E(t *testing.T) {
	targetURL := os.Getenv("SNOOZE_E2E_DATADOG_URL")
	if targetURL == "" {
		t.Skip("set SNOOZE_E2E_DATADOG_URL to run the Datadog end-to-end test")
	}

	payload := map[string]any{
		"alert_id":         "e2e-test-001",
		"title":            "[Triggered] E2E test alert from snooze plugin",
		"body":             "End-to-end test fired at " + time.Now().UTC().Format(time.RFC3339),
		"event_type":       "triggered",
		"alert_type":       "error",
		"alert_transition": "Triggered",
		"date":             time.Now().UnixMilli(),
		"org_id":           "0",
		"host":             "snooze-e2e-host",
		"tags":             "service:snooze-test,env:e2e,team:ops",
		"priority":         "normal",
		"aggreg_key":       "snooze-e2e-agg",
		"link":             "https://app.datadoghq.com/monitors/0",
	}

	body, err := json.Marshal(payload)
	require.NoError(t, err)

	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequest(http.MethodPost, targetURL, bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.True(t,
		resp.StatusCode >= 200 && resp.StatusCode < 300,
		fmt.Sprintf("expected 2xx, got %d", resp.StatusCode),
	)
}
