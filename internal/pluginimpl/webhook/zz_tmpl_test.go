package webhook

import (
	"testing"

	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

func TestProdTemplateRenders(t *testing.T) {
	body := `{"summary": "[System] Incident ${host} - ${message}", "labels": ["auto-incident"], "alert": {{ __self__ | tojson() }}}`
	rec := snoozetypes.Record{Host: "K8S ovh", Message: "Velero backup stale", Severity: "critical",
		Extra: map[string]any{"duplicates": int64(3)}}
	out, ct, err := renderBody(body, rec, plugins.NotificationPayload{Meta: map[string]any{"action_name": "Jira Ticket"}})
	t.Logf("err=%v ct=%q body=%s", err, ct, string(out))
}
