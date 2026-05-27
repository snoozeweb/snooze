package servicenow

// End-to-end tests for the ServiceNow notifier.
//
// These tests are gated behind environment variables so that `go test ./...`
// stays green with no external dependencies.  Set the variables described
// below and run:
//
//	go test -run E2E ./internal/pluginimpl/servicenow/...
//
// Prerequisites: a ServiceNow Personal Developer Instance (PDI) with a user
// that has the `itil` and `rest_api_explorer` roles.  See docs for details.

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// TestServiceNowE2E creates a real ServiceNow incident on a PDI.
//
// Required env vars:
//   - SNOOZE_E2E_SERVICENOW_INSTANCE – e.g. https://dev12345.service-now.com
//   - SNOOZE_E2E_SERVICENOW_USER     – ServiceNow username
//   - SNOOZE_E2E_SERVICENOW_PASSWORD – ServiceNow password
func TestServiceNowE2E(t *testing.T) {
	instance := os.Getenv("SNOOZE_E2E_SERVICENOW_INSTANCE")
	user := os.Getenv("SNOOZE_E2E_SERVICENOW_USER")
	password := os.Getenv("SNOOZE_E2E_SERVICENOW_PASSWORD")

	if instance == "" || user == "" || password == "" {
		t.Skip("set SNOOZE_E2E_SERVICENOW_INSTANCE, SNOOZE_E2E_SERVICENOW_USER, and SNOOZE_E2E_SERVICENOW_PASSWORD to run the ServiceNow end-to-end test")
	}

	p, err := factory(plugins.Metadata{Name: "servicenow"})
	require.NoError(t, err)
	require.NoError(t, p.PostInit(context.Background(), nil))

	rec := snoozetypes.Record{
		UID:      "snooze-e2e-001",
		Hash:     "snooze-e2e-hash-001",
		Host:     "e2e-test-host.local",
		Source:   "snooze-e2e",
		Severity: "warning",
		Message:  "Snooze ServiceNow E2E test incident — safe to close",
	}

	err = p.(plugins.Notifier).Send(context.Background(), rec, plugins.NotificationPayload{
		Meta: map[string]any{
			"instance_url": instance,
			"username":     user,
			"password":     password,
		},
	})
	require.NoError(t, err)
}
