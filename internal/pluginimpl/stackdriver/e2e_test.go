package stackdriver

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestStackdriverE2E posts a realistic GCP Monitoring "open" incident payload
// to a live snooze-server instance and asserts a 2xx response. Set
// SNOOZE_E2E_STACKDRIVER_URL to the full webhook URL before running, e.g.:
//
//	export SNOOZE_E2E_STACKDRIVER_URL=http://snooze.example.com/api/v1/webhook/stackdriver
//	go test -run TestStackdriverE2E ./internal/pluginimpl/stackdriver/...
func TestStackdriverE2E(t *testing.T) {
	url := os.Getenv("SNOOZE_E2E_STACKDRIVER_URL")
	if url == "" {
		t.Skip("set SNOOZE_E2E_STACKDRIVER_URL to run the Stackdriver end-to-end test")
	}

	payload := []byte(`{
		"incident": {
			"incident_id": "0.e2e-incident-1",
			"resource_name": "e2e-vm-1",
			"resource_id": "1234567890",
			"condition_name": "E2E CPU above 90%",
			"policy_name": "E2E High CPU policy",
			"state": "open",
			"severity": "critical",
			"summary": "E2E test: Stackdriver integration check",
			"url": "https://console.cloud.google.com/monitoring/alerting/incidents/0.e2e-incident-1",
			"started_at": 1700000000,
			"ended_at": null
		}
	}`)

	resp, err := http.Post(url, "application/json", bytes.NewReader(payload)) //nolint:gosec // intentional E2E call
	require.NoError(t, err)
	defer resp.Body.Close()

	require.True(t,
		resp.StatusCode >= 200 && resp.StatusCode < 300,
		fmt.Sprintf("expected 2xx, got %d", resp.StatusCode),
	)
}
