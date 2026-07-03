package core

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/db/sqlite"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// TestDemoBuildFlowRulesAndAggregate verifies every alert gets the always-on
// parser rule + a shift rule and the default aggregate, and that a non-critical
// alert (matching no notification) gets no notifications/actions.
func TestDemoBuildFlowRulesAndAggregate(t *testing.T) {
	doc := db.Document{"severity": "warning", "environment": "staging", "period": "day"}
	demoBuildFlow(doc, "", "")

	rules, ok := doc["rules"].([]string)
	if !ok || len(rules) != 2 || rules[0] != "Parse Host Components" || rules[1] != "Day Shift" {
		t.Fatalf("rules = %v, want [Parse Host Components, Day Shift]", doc["rules"])
	}
	if doc["aggregate"] != "Host and Message" {
		t.Fatalf("aggregate = %v, want Host and Message", doc["aggregate"])
	}
	if _, ok := doc["notifications"]; ok {
		t.Fatalf("warning alert should match no notification, got %v", doc["notifications"])
	}
	if _, ok := doc["actions"]; ok {
		t.Fatalf("warning alert should have no actions, got %v", doc["actions"])
	}
}

// TestDemoBuildFlowNightShift verifies the night period selects the Night Shift
// rule as the second matched rule.
func TestDemoBuildFlowNightShift(t *testing.T) {
	doc := db.Document{"severity": "info", "environment": "development", "period": "night"}
	demoBuildFlow(doc, "", "")

	rules := doc["rules"].([]string)
	if len(rules) != 2 || rules[1] != "Night Shift" {
		t.Fatalf("rules = %v, want Night Shift as the second rule", rules)
	}
}

// TestDemoBuildFlowCriticalProduction verifies a critical production alert
// matches both seeded notifications and fans out to both actions, in order.
func TestDemoBuildFlowCriticalProduction(t *testing.T) {
	doc := db.Document{"severity": "critical", "environment": "production", "period": "day"}
	demoBuildFlow(doc, "", "")

	notifs, ok := doc["notifications"].([]string)
	if !ok || len(notifs) != 2 || notifs[0] != "Critical Alerts" || notifs[1] != "Production Incidents" {
		t.Fatalf("notifications = %v, want [Critical Alerts, Production Incidents]", doc["notifications"])
	}

	actions, ok := doc["actions"].([]any)
	if !ok || len(actions) != 2 {
		t.Fatalf("actions = %v, want 2 entries", doc["actions"])
	}
	a0 := actions[0].(map[string]any)
	if a0["name"] != "Slack #ops-alerts" || a0["notification"] != "Critical Alerts" || a0["status"] != "success" {
		t.Fatalf("action[0] = %v, want Slack #ops-alerts/Critical Alerts/success", a0)
	}
	a1 := actions[1].(map[string]any)
	if a1["name"] != "Email Operations" || a1["notification"] != "Production Incidents" || a1["status"] != "success" {
		t.Fatalf("action[1] = %v, want Email Operations/Production Incidents/success", a1)
	}
}

// TestDemoBuildFlowCriticalNonProduction verifies a critical alert outside
// production matches only "Critical Alerts" (Production Incidents requires
// production), fanning out to a single action.
func TestDemoBuildFlowCriticalNonProduction(t *testing.T) {
	doc := db.Document{"severity": "critical", "environment": "staging", "period": "day"}
	demoBuildFlow(doc, "", "")

	notifs := doc["notifications"].([]string)
	if len(notifs) != 1 || notifs[0] != "Critical Alerts" {
		t.Fatalf("notifications = %v, want [Critical Alerts] only", notifs)
	}
	if actions := doc["actions"].([]any); len(actions) != 1 {
		t.Fatalf("actions = %v, want 1 entry", actions)
	}
}

// TestDemoBuildFlowSnoozedStopsPipeline verifies a snoozed alert still gets
// rules + aggregate but never notifications/actions — the snooze plugin
// terminates the pipeline before the notification plugin.
func TestDemoBuildFlowSnoozedStopsPipeline(t *testing.T) {
	doc := db.Document{
		"severity": "warning", "environment": "production", "period": "night",
		"snoozed": "Night Warning Suppression",
	}
	demoBuildFlow(doc, "", "")

	if _, ok := doc["rules"].([]string); !ok {
		t.Fatalf("snoozed alert should still get rules, got %v", doc["rules"])
	}
	if doc["aggregate"] != "Host and Message" {
		t.Fatalf("snoozed alert should still get aggregate, got %v", doc["aggregate"])
	}
	if _, ok := doc["notifications"]; ok {
		t.Fatalf("snoozed alert should fire no notifications, got %v", doc["notifications"])
	}
	if _, ok := doc["actions"]; ok {
		t.Fatalf("snoozed alert should fire no actions, got %v", doc["actions"])
	}
}

// TestDemoBuildFlowErrorInjection verifies the named action is stamped as an
// error while its sibling stays a success (and carries no error key).
func TestDemoBuildFlowErrorInjection(t *testing.T) {
	doc := db.Document{"severity": "critical", "environment": "production", "period": "night"}
	demoBuildFlow(doc, "Slack #ops-alerts", "webhook POST returned 503 Service Unavailable")

	actions := doc["actions"].([]any)
	a0 := actions[0].(map[string]any)
	if a0["name"] != "Slack #ops-alerts" || a0["status"] != "error" ||
		a0["error"] != "webhook POST returned 503 Service Unavailable" {
		t.Fatalf("action[0] = %v, want Slack #ops-alerts errored with the 503 reason", a0)
	}
	a1 := actions[1].(map[string]any)
	if a1["status"] != "success" {
		t.Fatalf("action[1] = %v, want success", a1)
	}
	if _, ok := a1["error"]; ok {
		t.Fatalf("successful action should carry no error key, got %v", a1)
	}
}

// --- end-to-end: SeedDemoData actually stamps the flow fields on stored records ---

// demoFindRecord returns the seeded record whose message contains msgSubstr.
func demoFindRecord(t *testing.T, recs []db.Document, msgSubstr string) db.Document {
	t.Helper()
	for _, r := range recs {
		if msg, _ := r["message"].(string); msg != "" && strings.Contains(msg, msgSubstr) {
			return r
		}
	}
	t.Fatalf("no seeded record with message containing %q", msgSubstr)
	return nil
}

// demoStrs coerces a DB-roundtripped string list ([]string or []any of strings).
func demoStrs(v any) []string {
	switch x := v.(type) {
	case []string:
		return x
	case []any:
		out := make([]string, 0, len(x))
		for _, e := range x {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// TestSeedDemoDataStampsFlowFields boots the demo seed against a real SQLite
// driver and confirms the flow-chart attribution survives the write/read
// round-trip on representative records.
func TestSeedDemoDataStampsFlowFields(t *testing.T) {
	t.Parallel()
	ctx := snoozetypes.WithTenant(context.Background(), snoozetypes.DefaultTenant)

	path := filepath.Join(t.TempDir(), "snooze.db")
	drv, err := sqlite.New(ctx, sqlite.Config{Path: path})
	if err != nil {
		t.Fatalf("sqlite.New: %v", err)
	}
	t.Cleanup(func() { _ = drv.Close() })

	if err := SeedDemoData(ctx, drv); err != nil {
		t.Fatalf("SeedDemoData: %v", err)
	}

	recs, _, err := drv.Search(ctx, "record", condition.Cond{}, db.Page{})
	if err != nil {
		t.Fatalf("search records: %v", err)
	}
	if len(recs) != 17 {
		t.Fatalf("seeded %d records, want 17", len(recs))
	}

	// A daytime critical production alert: both notifications, both actions OK.
	r := demoFindRecord(t, recs, "HTTP 5xx error rate")
	if got := demoStrs(r["rules"]); len(got) != 2 || got[0] != "Parse Host Components" || got[1] != "Day Shift" {
		t.Fatalf("rules = %v, want [Parse Host Components, Day Shift]", r["rules"])
	}
	if r["aggregate"] != "Host and Message" {
		t.Fatalf("aggregate = %v, want Host and Message", r["aggregate"])
	}
	if got := demoStrs(r["notifications"]); len(got) != 2 ||
		got[0] != "Critical Alerts" || got[1] != "Production Incidents" {
		t.Fatalf("notifications = %v, want [Critical Alerts, Production Incidents]", r["notifications"])
	}
	if acts, ok := r["actions"].([]any); !ok || len(acts) != 2 {
		t.Fatalf("actions = %v, want 2 entries", r["actions"])
	}

	// The escalated disk-full alert: its Slack action is stamped as an error.
	esc := demoFindRecord(t, recs, "Disk space on /var/lib/postgresql")
	acts, ok := esc["actions"].([]any)
	if !ok || len(acts) != 2 {
		t.Fatalf("escalated actions = %v, want 2 entries", esc["actions"])
	}
	slack, _ := acts[0].(map[string]any)
	if slack["name"] != "Slack #ops-alerts" || slack["status"] != "error" ||
		slack["error"] != "webhook POST returned 503 Service Unavailable" {
		t.Fatalf("escalated Slack action = %v, want errored 503", slack)
	}

	// A warning alert matches no notification: rules present, no actions.
	warn := demoFindRecord(t, recs, "High memory pressure")
	if got := demoStrs(warn["rules"]); len(got) == 0 {
		t.Fatalf("warning alert should still get rules, got %v", warn["rules"])
	}
	if _, ok := warn["notifications"]; ok {
		t.Fatalf("warning alert should match no notification, got %v", warn["notifications"])
	}
	if _, ok := warn["actions"]; ok {
		t.Fatalf("warning alert should have no actions, got %v", warn["actions"])
	}

	// A snoozed alert terminates before notifications: rules + aggregate only.
	snz := demoFindRecord(t, recs, "SSL handshake timeout spike")
	if snz["snoozed"] != "Night Warning Suppression" {
		t.Fatalf("snoozed = %v, want Night Warning Suppression", snz["snoozed"])
	}
	if snz["aggregate"] != "Host and Message" {
		t.Fatalf("snoozed aggregate = %v, want Host and Message", snz["aggregate"])
	}
	if _, ok := snz["notifications"]; ok {
		t.Fatalf("snoozed alert should fire no notifications, got %v", snz["notifications"])
	}
	if _, ok := snz["actions"]; ok {
		t.Fatalf("snoozed alert should fire no actions, got %v", snz["actions"])
	}
}
