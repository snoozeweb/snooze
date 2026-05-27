package ntfy

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// TestNtfyE2E sends a real notification to a live ntfy server.
//
// Environment variables:
//
//	SNOOZE_E2E_NTFY_TOPIC   – ntfy topic name (required; test is skipped when absent)
//	SNOOZE_E2E_NTFY_SERVER  – ntfy server base URL (optional; defaults to https://ntfy.sh)
//	SNOOZE_E2E_NTFY_TOKEN   – Bearer token (optional)
//	SNOOZE_E2E_NTFY_USERNAME – Basic auth username (optional)
//	SNOOZE_E2E_NTFY_PASSWORD – Basic auth password (optional)
//
// To run:
//
//	export SNOOZE_E2E_NTFY_TOPIC=my-snooze-e2e
//	go test -run TestNtfyE2E ./internal/pluginimpl/ntfy/...
func TestNtfyE2E(t *testing.T) {
	topic := os.Getenv("SNOOZE_E2E_NTFY_TOPIC")
	if topic == "" {
		t.Skip("set SNOOZE_E2E_NTFY_TOPIC to run the ntfy end-to-end test")
	}

	server := os.Getenv("SNOOZE_E2E_NTFY_SERVER")
	if server == "" {
		server = defaultServer
	}

	meta := map[string]any{
		"server":  server,
		"topic":   topic,
		"title":   "Snooze E2E test",
		"message": "ntfy plugin end-to-end test fired at {{ .Timestamp }}",
		"timeout": "15s",
	}
	if tok := os.Getenv("SNOOZE_E2E_NTFY_TOKEN"); tok != "" {
		meta["token"] = tok
	}
	if u := os.Getenv("SNOOZE_E2E_NTFY_USERNAME"); u != "" {
		meta["username"] = u
		meta["password"] = os.Getenv("SNOOZE_E2E_NTFY_PASSWORD")
	}

	p, err := factory(plugins.Metadata{Name: "ntfy"})
	require.NoError(t, err)
	require.NoError(t, p.PostInit(context.Background(), nil))

	rec := snoozetypes.Record{
		UID:       "e2e-ntfy-001",
		Host:      "snooze-ci",
		Source:    "e2e",
		Severity:  "info",
		Message:   "ntfy plugin E2E test",
		Timestamp: time.Now().UTC(),
	}

	err = p.(plugins.Notifier).Send(context.Background(), rec, plugins.NotificationPayload{Meta: meta})
	require.NoError(t, err, "ntfy E2E send must succeed")
}
