package azuremonitor

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestAzureMonitorE2E posts a realistic Common Alert Schema payload to a live
// snooze-server instance and asserts a 2xx response. Set
// SNOOZE_E2E_AZUREMONITOR_URL to the full webhook URL before running, e.g.:
//
//	export SNOOZE_E2E_AZUREMONITOR_URL=http://snooze.example.com/api/v1/webhook/azuremonitor
//	go test -run TestAzureMonitorE2E ./internal/pluginimpl/azuremonitor/...
func TestAzureMonitorE2E(t *testing.T) {
	url := os.Getenv("SNOOZE_E2E_AZUREMONITOR_URL")
	if url == "" {
		t.Skip("set SNOOZE_E2E_AZUREMONITOR_URL to run the Azure Monitor end-to-end test")
	}

	payload := []byte(`{
		"schemaId": "azureMonitorCommonAlertSchema",
		"data": {
			"essentials": {
				"alertId": "/subscriptions/e2e-sub/providers/Microsoft.AlertsManagement/alerts/e2e-alert-1",
				"alertRule": "E2E Test Alert",
				"severity": "Sev2",
				"signalType": "Metric",
				"monitorCondition": "Fired",
				"monitoringService": "Platform",
				"alertTargetIDs": [
					"/subscriptions/e2e-sub/resourceGroups/rg-e2e/providers/Microsoft.Compute/virtualMachines/e2e-vm-1"
				],
				"firedDateTime": "2026-05-27T12:00:00Z",
				"description": "E2E test: Azure Monitor integration check"
			},
			"alertContext": {
				"conditionType": "SingleResourceMultipleMetricCriteria",
				"properties": {"threshold": "80"}
			}
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
