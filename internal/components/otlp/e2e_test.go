package otlp

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"
)

// TestOTLPE2E posts a realistic OTLP-JSON logs payload to a running
// snooze-otlp /v1/logs endpoint and asserts a 200 OK. It is gated on
// SNOOZE_E2E_OTLP_URL so `go test ./...` stays green when the env var is unset.
//
// Example:
//
//	export SNOOZE_E2E_OTLP_URL="http://127.0.0.1:4318/v1/logs"
//	go test -run E2E ./internal/components/otlp/...
func TestOTLPE2E(t *testing.T) {
	url := os.Getenv("SNOOZE_E2E_OTLP_URL")
	if url == "" {
		t.Skip("set SNOOZE_E2E_OTLP_URL (e.g. http://127.0.0.1:4318/v1/logs) to run the OTLP end-to-end test")
	}

	payload := fmt.Sprintf(`{
  "resourceLogs": [{
    "resource": {"attributes": [
      {"key": "host.name", "value": {"stringValue": "e2e-host"}},
      {"key": "service.name", "value": {"stringValue": "snooze-otlp-e2e"}}
    ]},
    "scopeLogs": [{"logRecords": [{
      "timeUnixNano": "%d",
      "severityNumber": 17,
      "severityText": "ERROR",
      "body": {"stringValue": "snooze-otlp end-to-end test alert"},
      "attributes": [{"key": "test", "value": {"boolValue": true}}]
    }]}]
  }]
}`, time.Now().UnixNano())

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader([]byte(payload)))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}
