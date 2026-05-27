package sentry

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestSentryE2E posts a realistic Sentry webhook payload to a running
// snooze-server and asserts a 2xx response.
//
// Gate: set SNOOZE_E2E_SENTRY_URL to the full webhook endpoint URL, e.g.
//
//	export SNOOZE_E2E_SENTRY_URL="http://localhost:5200/api/v1/webhook/sentry"
//	go test -run TestSentryE2E ./internal/pluginimpl/sentry/...
func TestSentryE2E(t *testing.T) {
	endpointURL := os.Getenv("SNOOZE_E2E_SENTRY_URL")
	if endpointURL == "" {
		t.Skip("set SNOOZE_E2E_SENTRY_URL to run the Sentry end-to-end test")
	}

	// Realistic legacy Sentry webhook payload.
	body := fmt.Sprintf(`{
		"id": "e2e-test-%d",
		"project": "e2e-test-project",
		"project_name": "E2E Test Project",
		"culprit": "main in e2e_test.go",
		"message": "E2E test alert from snooze sentry plugin",
		"url": "https://sentry.io/organizations/example/issues/99999/",
		"level": "error",
		"server_name": "e2e-test-host",
		"event": {
			"event_id": "e2e000000000000000000000000000001",
			"tags": [["server_name", "e2e-test-host"], ["environment", "test"]],
			"environment": "test",
			"release": "e2e-1.0.0"
		}
	}`, time.Now().UnixNano())

	resp, err := http.Post(endpointURL, "application/json", bytes.NewBufferString(body)) //nolint:noctx
	require.NoError(t, err)
	defer resp.Body.Close()

	require.True(t,
		resp.StatusCode >= 200 && resp.StatusCode < 300,
		"expected 2xx, got %d", resp.StatusCode,
	)
}

// TestSentryE2EModern posts a modern Integration-style Sentry payload to
// a running snooze-server and asserts a 2xx response.
func TestSentryE2EModern(t *testing.T) {
	endpointURL := os.Getenv("SNOOZE_E2E_SENTRY_URL")
	if endpointURL == "" {
		t.Skip("set SNOOZE_E2E_SENTRY_URL to run the Sentry end-to-end test")
	}

	body := `{
		"action": "triggered",
		"data": {
			"issue": {
				"title": "E2E modern test: ZeroDivisionError",
				"culprit": "divide in e2e_test.go",
				"level": "warning",
				"permalink": "https://sentry.io/organizations/example/issues/88888/",
				"project": {
					"slug": "e2e-modern-project",
					"name": "E2E Modern Project"
				}
			},
			"triggered_rule": "E2E alert rule"
		}
	}`

	resp, err := http.Post(endpointURL, "application/json", bytes.NewBufferString(body)) //nolint:noctx
	require.NoError(t, err)
	defer resp.Body.Close()

	require.True(t,
		resp.StatusCode >= 200 && resp.StatusCode < 300,
		"expected 2xx, got %d", resp.StatusCode,
	)
}
