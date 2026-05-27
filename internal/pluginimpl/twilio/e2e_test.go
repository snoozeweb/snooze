package twilio

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// TestTwilioE2E sends one real SMS via the Twilio REST API. It is skipped
// unless all four SNOOZE_E2E_TWILIO_* environment variables are set.
//
// Set-up:
//
//	export SNOOZE_E2E_TWILIO_SID="ACxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
//	export SNOOZE_E2E_TWILIO_TOKEN="<auth token from console>"
//	export SNOOZE_E2E_TWILIO_FROM="+1XXXXXXXXXX"   # your Twilio number (E.164)
//	export SNOOZE_E2E_TWILIO_TO="+1XXXXXXXXXX"     # verified destination (E.164)
//	go test -run TestTwilioE2E ./internal/pluginimpl/twilio/...
func TestTwilioE2E(t *testing.T) {
	sid := os.Getenv("SNOOZE_E2E_TWILIO_SID")
	token := os.Getenv("SNOOZE_E2E_TWILIO_TOKEN")
	from := os.Getenv("SNOOZE_E2E_TWILIO_FROM")
	to := os.Getenv("SNOOZE_E2E_TWILIO_TO")

	if sid == "" || token == "" || from == "" || to == "" {
		t.Skip("set SNOOZE_E2E_TWILIO_SID, SNOOZE_E2E_TWILIO_TOKEN, " +
			"SNOOZE_E2E_TWILIO_FROM and SNOOZE_E2E_TWILIO_TO to run the Twilio end-to-end test")
	}

	p, err := factory(plugins.Metadata{Name: "twilio"})
	require.NoError(t, err)
	require.NoError(t, p.PostInit(context.Background(), nil))

	rec := snoozetypes.Record{
		UID:      "e2e-test-1",
		Host:     "ci.example.com",
		Severity: "info",
		Message:  "Twilio e2e test from snooze",
	}

	err = p.(plugins.Notifier).Send(context.Background(), rec, plugins.NotificationPayload{
		Meta: map[string]any{
			"account_sid": sid,
			"auth_token":  token,
			"from":        from,
			"to":          to,
			"mode":        "sms",
		},
	})
	require.NoError(t, err)
}
