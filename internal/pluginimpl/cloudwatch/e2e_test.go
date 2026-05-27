package cloudwatch

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCloudWatchE2E posts a realistic captured SNS Notification body to a
// running snooze-server's /api/v1/webhook/cloudwatch endpoint and asserts an
// HTTP 2xx response.
//
// Set SNOOZE_E2E_CLOUDWATCH_URL to the full URL of the endpoint, e.g.:
//
//	export SNOOZE_E2E_CLOUDWATCH_URL="http://localhost:5200/api/v1/webhook/cloudwatch"
//	go test -run TestCloudWatchE2E ./internal/pluginimpl/cloudwatch/...
func TestCloudWatchE2E(t *testing.T) {
	targetURL := os.Getenv("SNOOZE_E2E_CLOUDWATCH_URL")
	if targetURL == "" {
		t.Skip("set SNOOZE_E2E_CLOUDWATCH_URL to run the CloudWatch end-to-end test")
	}

	// Realistic SNS Notification envelope carrying a CloudWatch ALARM payload.
	alarmJSON, err := json.Marshal(map[string]any{
		"AlarmName":        "e2e-high-cpu",
		"AlarmDescription": "End-to-end test alarm: CPU too high",
		"NewStateValue":    "ALARM",
		"NewStateReason":   "Threshold Crossed: 1 datapoint [91.5 (15/01/24 12:00:00)] was greater than or equal to the threshold (90.0).",
		"Region":           "us-east-1",
		"StateChangeTime":  "2024-01-15T12:00:00.000+0000",
		"Trigger": map[string]any{
			"Namespace":  "AWS/EC2",
			"MetricName": "CPUUtilization",
			"Dimensions": []map[string]string{
				{"name": "InstanceId", "value": "i-0e2e000000000001"},
			},
		},
	})
	require.NoError(t, err)

	envelope, err := json.Marshal(map[string]string{
		"Type":      "Notification",
		"MessageId": "e2e-test-001",
		"TopicArn":  "arn:aws:sns:us-east-1:123456789012:snooze-cloudwatch-e2e",
		"Subject":   "ALARM: e2e-high-cpu entered ALARM state",
		"Message":   string(alarmJSON),
		"Timestamp": "2024-01-15T12:00:01.000Z",
	})
	require.NoError(t, err)

	req, err := http.NewRequest(http.MethodPost, targetURL, strings.NewReader(string(envelope)))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-amz-sns-message-type", "Notification")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.True(t, resp.StatusCode >= 200 && resp.StatusCode < 300,
		"expected 2xx from snooze-server, got %d", resp.StatusCode)
}
