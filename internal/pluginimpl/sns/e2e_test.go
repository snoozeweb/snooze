package sns

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/plugins"
)

// TestSNSE2E publishes one real message to a real SNS topic. It is gated on
// four environment variables and skips when any is unset, so `go test ./...`
// stays green without AWS credentials.
//
//	SNOOZE_E2E_SNS_REGION             e.g. eu-west-1
//	SNOOZE_E2E_SNS_TOPIC_ARN          arn:aws:sns:<region>:<account>:<topic>
//	SNOOZE_E2E_SNS_ACCESS_KEY_ID      IAM access key id with sns:Publish
//	SNOOZE_E2E_SNS_SECRET_ACCESS_KEY  matching secret access key
//	SNOOZE_E2E_SNS_SESSION_TOKEN      (optional) STS session token
//	SNOOZE_E2E_SNS_ENDPOINT           (optional) endpoint override (LocalStack)
func TestSNSE2E(t *testing.T) {
	region := os.Getenv("SNOOZE_E2E_SNS_REGION")
	topicArn := os.Getenv("SNOOZE_E2E_SNS_TOPIC_ARN")
	accessKeyID := os.Getenv("SNOOZE_E2E_SNS_ACCESS_KEY_ID")
	secretAccessKey := os.Getenv("SNOOZE_E2E_SNS_SECRET_ACCESS_KEY")

	if region == "" || topicArn == "" || accessKeyID == "" || secretAccessKey == "" {
		t.Skip("set SNOOZE_E2E_SNS_REGION, SNOOZE_E2E_SNS_TOPIC_ARN, SNOOZE_E2E_SNS_ACCESS_KEY_ID and SNOOZE_E2E_SNS_SECRET_ACCESS_KEY to run the SNS end-to-end test")
	}

	p, err := factory(plugins.Metadata{Name: "sns"})
	require.NoError(t, err)
	require.NoError(t, p.PostInit(context.Background(), nil))

	meta := map[string]any{
		"region":            region,
		"topic_arn":         topicArn,
		"access_key_id":     accessKeyID,
		"secret_access_key": secretAccessKey,
		"subject":           "Snooze SNS e2e",
		"message":           "Snooze SNS end-to-end test: {{ .Severity }} on {{ .Host }} — {{ .Message }}",
	}
	if v := os.Getenv("SNOOZE_E2E_SNS_SESSION_TOKEN"); v != "" {
		meta["session_token"] = v
	}
	if v := os.Getenv("SNOOZE_E2E_SNS_ENDPOINT"); v != "" {
		meta["endpoint"] = v
	}

	err = p.(plugins.Notifier).Send(context.Background(), sampleRecord(), plugins.NotificationPayload{Meta: meta})
	require.NoError(t, err)
}
