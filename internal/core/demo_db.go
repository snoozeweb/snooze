package core

import (
	"context"
	"crypto/md5" //nolint:gosec
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/google/uuid"
	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// demoMarkerField is the general-collection field that prevents a second seed run.
const demoMarkerField = "demo_db"

// demoHostRE mirrors the "Parse Host Components" rule: ^<service>-<tier>-<instance_num>.
var demoHostRE = regexp.MustCompile(`^(?P<service>[a-z]+)-(?P<tier>[a-z]+)-(?P<instance_num>\d+)`)

// SeedDemoData populates the default tenant with a rich demonstration dataset:
// environments, users, rules, actions, notifications, snooze filters, 17 alert
// records in mixed states, and comments. The seeding is idempotent — a
// demo_db marker in the "general" collection guards against repeat runs.
//
// ctx MUST be scoped to the default tenant (boot passes seedCtx).
func SeedDemoData(ctx context.Context, drv db.Driver) error {
	if drv == nil {
		return errors.New("demo_db: nil driver")
	}

	// Idempotency: skip if already seeded.
	docs, _, err := drv.Search(ctx, generalCollection, condition.Cond{}, db.Page{})
	if err == nil {
		for _, d := range docs {
			if v, ok := d[demoMarkerField].(bool); ok && v {
				return nil
			}
		}
	}

	now := time.Now().UTC()

	// Pre-generate UIDs for the 17 alert records so that comments can
	// reference specific alerts by UID before the records are written.
	const numRecords = 17
	rUIDs := make([]string, numRecords)
	for i := range rUIDs {
		rUIDs[i] = uuid.NewString()
	}

	if err := seedDemoEnvironments(ctx, drv); err != nil {
		return fmt.Errorf("demo_db: environments: %w", err)
	}
	if err := seedDemoUsers(ctx, drv); err != nil {
		return fmt.Errorf("demo_db: users: %w", err)
	}
	if err := seedDemoRules(ctx, drv); err != nil {
		return fmt.Errorf("demo_db: rules: %w", err)
	}
	if err := seedDemoActions(ctx, drv); err != nil {
		return fmt.Errorf("demo_db: actions: %w", err)
	}
	if err := seedDemoNotifications(ctx, drv); err != nil {
		return fmt.Errorf("demo_db: notifications: %w", err)
	}
	if err := seedDemoSnoozes(ctx, drv); err != nil {
		return fmt.Errorf("demo_db: snoozes: %w", err)
	}
	if err := seedDemoRecords(ctx, drv, now, rUIDs); err != nil {
		return fmt.Errorf("demo_db: records: %w", err)
	}
	if err := seedDemoComments(ctx, drv, now, rUIDs); err != nil {
		return fmt.Errorf("demo_db: comments: %w", err)
	}

	if _, err := drv.Write(ctx, generalCollection, []db.Document{{demoMarkerField: true}},
		db.WriteOptions{UpdateTime: true}); err != nil {
		return fmt.Errorf("demo_db: write marker: %w", err)
	}
	return nil
}

// --- environments ---

func seedDemoEnvironments(ctx context.Context, drv db.Driver) error {
	envs := []db.Document{
		{
			"name":       "production",
			"color":      "#e53935",
			"condition":  condition.Equals("environment", "production").ToList(),
			"tree_order": 1,
		},
		{
			"name":       "staging",
			"color":      "#fb8c00",
			"condition":  condition.Equals("environment", "staging").ToList(),
			"tree_order": 2,
		},
		{
			"name":       "development",
			"color":      "#43a047",
			"condition":  condition.Equals("environment", "development").ToList(),
			"tree_order": 3,
		},
	}
	_, err := drv.Write(ctx, "environment", envs, db.WriteOptions{
		Primary:    []string{"name"},
		UpdateTime: true,
	})
	return err
}

// --- users ---

func seedDemoUsers(ctx context.Context, drv db.Driver) error {
	type userDef struct {
		name, password string
		roles          []string
	}
	defs := []userDef{
		{"alice.martin", "DemoPass1!", []string{"admin"}},
		{"bob.chen", "DemoPass2!", []string{"viewer"}},
		{"charlie.ops", "DemoPass3!", []string{"notifications"}},
	}
	now := time.Now().UTC().Format(time.RFC3339)
	docs := make([]db.Document, 0, len(defs))
	for _, u := range defs {
		hash, err := bcrypt.GenerateFromPassword([]byte(u.password), bcrypt.DefaultCost)
		if err != nil {
			return fmt.Errorf("bcrypt %s: %w", u.name, err)
		}
		docs = append(docs, db.Document{
			"tenant_id":  snoozetypes.DefaultTenant,
			"name":       u.name,
			"method":     auth.LocalMethod,
			"enabled":    true,
			"password":   string(hash),
			"roles":      u.roles,
			"groups":     []string{},
			"created_at": now,
		})
	}
	_, err := drv.Write(ctx, auth.LocalCollection, docs, db.WriteOptions{
		Primary:    []string{"name", "method"},
		UpdateTime: true,
	})
	return err
}

// --- rules ---

func seedDemoRules(ctx context.Context, drv db.Driver) error {
	// Conditions stored in the list form that the rule plugin's parseCondition
	// accepts via condition.FromList.
	matchesCond := func(field, regex string) any {
		return condition.Cond{Op: condition.OpMatches, Field: field, Value: regex}.ToList()
	}
	rules := []db.Document{
		{
			// Mirrors the "Parse Host Components" rule: REGEX_PARSE on host
			// sets service, tier, instance_num on every passing alert.
			"name":       "Parse Host Components",
			"enabled":    true,
			"condition":  []any{},
			"modifications": []any{
				[]any{"REGEX_PARSE", "host", `^(?P<service>[a-z]+)-(?P<tier>[a-z]+)-(?P<instance_num>\d+)`},
			},
			"tree_order": 1,
		},
		{
			// Mirrors the "Day Shift" rule: MATCHES on timestamp's Go string
			// representation ("2006-01-02 15:04:05 +0000 UTC") for hours 08–19.
			"name":    "Day Shift",
			"enabled": true,
			"condition": matchesCond("timestamp", ` (0[89]|1[0-9]):`),
			"modifications": []any{
				[]any{"SET", "period", "day"},
			},
			"tree_order": 2,
		},
		{
			// Mirrors the "Night Shift" rule: hours 00–07 and 20–23.
			"name":    "Night Shift",
			"enabled": true,
			"condition": matchesCond("timestamp", ` (0[0-7]|2[0-3]):`),
			"modifications": []any{
				[]any{"SET", "period", "night"},
			},
			"tree_order": 3,
		},
	}
	_, err := drv.Write(ctx, "rule", rules, db.WriteOptions{
		Primary:    []string{"name"},
		UpdateTime: true,
	})
	return err
}

// --- actions ---

func seedDemoActions(ctx context.Context, drv db.Driver) error {
	actions := []db.Document{
		{
			"name": "Slack #ops-alerts",
			"action": map[string]any{
				"selected": "webhook",
				"subcontent": map[string]any{
					"url":    "https://hooks.slack.com/services/demo/demo/demo",
					"method": "POST",
				},
			},
		},
		{
			"name": "Email Operations",
			"action": map[string]any{
				"selected": "mail",
				"subcontent": map[string]any{
					"to":   "ops@example.com",
					"from": "snooze@example.com",
				},
			},
		},
	}
	_, err := drv.Write(ctx, "action", actions, db.WriteOptions{
		Primary:    []string{"name"},
		UpdateTime: true,
	})
	return err
}

// --- notifications ---

func seedDemoNotifications(ctx context.Context, drv db.Driver) error {
	// Notification conditions use the JSON object form because decodeEntry
	// round-trips through json.Marshal/Unmarshal into condition.Cond.
	criticalCond := condAsMap(condition.Equals("severity", "critical"))
	prodCriticalCond := condAsMap(condition.And(
		condition.Equals("environment", "production"),
		condition.Equals("severity", "critical"),
	))
	notifications := []db.Document{
		{
			"name":      "Critical Alerts",
			"enabled":   true,
			"condition": criticalCond,
			"actions":   []string{"Slack #ops-alerts"},
		},
		{
			"name":      "Production Incidents",
			"enabled":   true,
			"condition": prodCriticalCond,
			"actions":   []string{"Email Operations"},
		},
	}
	_, err := drv.Write(ctx, "notification", notifications, db.WriteOptions{
		Primary:    []string{"name"},
		UpdateTime: true,
	})
	return err
}

// --- snooze filters ---

func seedDemoSnoozes(ctx context.Context, drv db.Driver) error {
	warnCond := condition.Equals("severity", "warning").ToList()
	devInfoCond := condition.And(
		condition.Equals("environment", "development"),
		condition.Equals("severity", "info"),
	).ToList()
	stagingCond := condition.Equals("environment", "staging").ToList()

	snoozes := []db.Document{
		{
			"name":      "Night Warning Suppression",
			"enabled":   true,
			"discard":   false,
			"condition": warnCond,
			"time_constraints": map[string]any{
				"time": []any{
					map[string]any{"from": "20:00", "until": "08:00"},
				},
			},
			"hits_enabled": true,
			"hits":         int64(0),
		},
		{
			"name":         "Dev Noise Reduction",
			"enabled":      true,
			"discard":      false,
			"condition":    devInfoCond,
			"hits_enabled": true,
			"hits":         int64(0),
		},
		{
			"name":      "Staging Weekend Maintenance",
			"enabled":   true,
			"discard":   false,
			"condition": stagingCond,
			"time_constraints": map[string]any{
				// Sunday=0, Saturday=6 (Go's time.Weekday).
				"weekdays": []any{
					map[string]any{"weekdays": []any{0, 6}},
				},
			},
			"hits_enabled": true,
			"hits":         int64(0),
		},
	}
	_, err := drv.Write(ctx, "snooze", snoozes, db.WriteOptions{
		Primary:    []string{"name"},
		UpdateTime: true,
	})
	return err
}

// --- records (alerts) ---

// seedDemoRecords writes 12 regular + 5 snoozed alerts into the record
// collection. Each record is enriched exactly as the live pipeline would:
//   - "Parse Host Components" rule → service, tier, instance_num
//   - "Day/Night Shift" rules     → period
//   - "Host and Message" aggrule  → hash
//
// Snoozed records additionally carry a snoozed field matching the name of the
// first snooze filter that would have matched them at processing time.
func seedDemoRecords(ctx context.Context, drv db.Driver, now time.Time, rUIDs []string) error {
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	yesterday := today.AddDate(0, 0, -1)
	twoDaysAgo := today.AddDate(0, 0, -2)
	lastSat := demoLastSaturday(now)

	type alertDef struct {
		host, source, severity, environment, state, message string
		tags                                                []string
		ts                                                  time.Time
		snoozeRule                                          string // non-empty → snoozed alert
	}

	defs := []alertDef{
		// --- open ---
		{
			host: "web-prod-01", source: "alertmanager", severity: "critical",
			environment: "production", state: "open",
			message: "HTTP 5xx error rate above 10% for 5 minutes",
			tags:    []string{"critical"},
			ts:      yesterday.Add(9*time.Hour + 15*time.Minute),
		},
		{
			host: "db-prod-01", source: "prometheus", severity: "critical",
			environment: "production", state: "open",
			message: "PostgreSQL connection pool exhausted (95% used)",
			tags:    []string{"critical"},
			ts:      yesterday.Add(11*time.Hour + 40*time.Minute),
		},
		{
			host: "web-prod-02", source: "prometheus", severity: "warning",
			environment: "production", state: "open",
			message: "High memory pressure — swap usage at 70%",
			ts:      yesterday.Add(14*time.Hour + 20*time.Minute),
		},
		{
			host: "api-staging-01", source: "grafana", severity: "warning",
			environment: "staging", state: "open",
			message: "API response time P99 > 2s on /api/v1/alerts",
			ts:      yesterday.Add(9*time.Hour + 5*time.Minute),
		},
		{
			host: "cache-staging-01", source: "prometheus", severity: "warning",
			environment: "staging", state: "open",
			message: "Redis memory usage above 80% (12 GB / 15 GB)",
			ts:      yesterday.Add(13*time.Hour + 30*time.Minute),
		},
		{
			host: "worker-dev-01", source: "prometheus", severity: "info",
			environment: "development", state: "open",
			message: "Background job queue depth > 100 (currently 143)",
			ts:      yesterday.Add(15*time.Hour + 45*time.Minute),
		},
		// --- ack (index 6 = R07: TLS cert — receives a comment) ---
		{
			host: "web-prod-02", source: "alertmanager", severity: "critical",
			environment: "production", state: "ack",
			message: "TLS certificate expiring in 7 days (SAN: *.acme.com)",
			tags:    []string{"critical"},
			ts:      yesterday.Add(23*time.Hour + 15*time.Minute),
		},
		// --- ack (index 7 = R08: slow query — receives a comment) ---
		{
			host: "api-staging-01", source: "grafana", severity: "warning",
			environment: "staging", state: "ack",
			message: "Slow query detected: p99 execution time 3.2s",
			ts:      twoDaysAgo.Add(16*time.Hour + 30*time.Minute),
		},
		{
			host: "ci-dev-01", source: "heartbeat", severity: "info",
			environment: "development", state: "ack",
			message: "CI runner offline for 15 minutes",
			ts:      yesterday.Add(8*time.Hour + 30*time.Minute),
		},
		// --- esc (index 9 = R10: disk full — receives two comments) ---
		{
			host: "db-prod-01", source: "prometheus", severity: "critical",
			environment: "production", state: "esc",
			message: "Disk space on /var/lib/postgresql at 92% — escalated to DBA",
			tags:    []string{"critical", "escalated"},
			ts:      yesterday.Add(7*time.Hour + 45*time.Minute),
		},
		// --- close (index 10 = R11: CPU — receives a comment) ---
		{
			host: "web-prod-01", source: "alertmanager", severity: "warning",
			environment: "production", state: "close",
			message: "CPU load above 80% for 10 minutes — RESOLVED",
			tags:    []string{"false-positive"},
			ts:      twoDaysAgo.Add(16 * time.Hour),
		},
		{
			host: "worker-dev-01", source: "grafana", severity: "warning",
			environment: "development", state: "close",
			message: "Memory spike to 85% — self-resolved after GC",
			ts:      twoDaysAgo.Add(21*time.Hour + 30*time.Minute),
		},

		// --- snoozed records (indices 12–16) ---
		// S01: warning at night → Night Warning Suppression
		{
			host: "web-prod-02", source: "prometheus", severity: "warning",
			environment: "production", state: "open",
			message:    "SSL handshake timeout spike — 3% of connections affected",
			ts:         yesterday.Add(23*time.Hour + 15*time.Minute),
			snoozeRule: "Night Warning Suppression",
		},
		// S02: warning at night → Night Warning Suppression
		{
			host: "cache-staging-01", source: "prometheus", severity: "warning",
			environment: "staging", state: "open",
			message:    "Swap usage above 50% — memory pressure expected at night",
			ts:         yesterday.Add(22*time.Hour + 40*time.Minute),
			snoozeRule: "Night Warning Suppression",
		},
		// S03: dev+info, always → Dev Noise Reduction
		{
			host: "worker-dev-01", source: "prometheus", severity: "info",
			environment: "development", state: "open",
			message:    "Unused build cache detected — 2.3 GB recoverable",
			ts:         yesterday.Add(6*time.Hour + 30*time.Minute),
			snoozeRule: "Dev Noise Reduction",
		},
		// S04: dev+info, always → Dev Noise Reduction
		{
			host: "ci-dev-01", source: "prometheus", severity: "info",
			environment: "development", state: "open",
			message:    "Test suite flakiness: 2/50 tests non-deterministic",
			ts:         yesterday.Add(7*time.Hour + 15*time.Minute),
			snoozeRule: "Dev Noise Reduction",
		},
		// S05: staging on a weekend → Staging Weekend Maintenance
		{
			host: "api-staging-01", source: "prometheus", severity: "warning",
			environment: "staging", state: "open",
			message:    "Database vacuum running — elevated query times expected",
			ts:         lastSat,
			snoozeRule: "Staging Weekend Maintenance",
		},
	}

	docs := make([]db.Document, 0, len(defs))
	for i, def := range defs {
		doc := db.Document{
			"uid":         rUIDs[i],
			"host":        def.host,
			"source":      def.source,
			"severity":    def.severity,
			"environment": def.environment,
			"state":       def.state,
			"message":     def.message,
			"timestamp":   def.ts,
			"date_epoch":  def.ts.Unix(),
		}
		if len(def.tags) > 0 {
			doc["tags"] = def.tags
		} else {
			doc["tags"] = []string{}
		}
		// Apply rule enrichment: host components, period, hash, plugins.
		demoBuildEnrichment(doc, def.ts)
		// Apply snooze attribution if this alert was captured by a filter.
		if def.snoozeRule != "" {
			doc["snoozed"] = def.snoozeRule
		}
		docs = append(docs, doc)
	}

	_, err := drv.Write(ctx, "record", docs, db.WriteOptions{
		Primary:    []string{"uid"},
		UpdateTime: true,
	})
	return err
}

// --- comments ---

func seedDemoComments(ctx context.Context, drv db.Driver, now time.Time, rUIDs []string) error {
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	yesterday := today.AddDate(0, 0, -1)
	twoDaysAgo := today.AddDate(0, 0, -2)

	type commentDef struct {
		recordUID   string
		commentType string // "ack", "esc", "close", or "" for a regular note
		user        string
		message     string
		ts          time.Time
	}
	defs := []commentDef{
		// R07 (TLS cert, ack): alice acknowledges with ticket reference
		{
			recordUID:   rUIDs[6],
			commentType: "ack",
			user:        "alice.martin",
			message:     "Cert renewal ticket created — INFRA-4521. Let's Encrypt auto-renewal in progress. ETA 24 h.",
			ts:          yesterday.Add(23*time.Hour + 25*time.Minute),
		},
		// R08 (slow query, ack): bob acknowledges with root-cause
		{
			recordUID:   rUIDs[7],
			commentType: "ack",
			user:        "bob.chen",
			message:     "Acked — query plan regression from yesterday's index drop. Rebuild scheduled.",
			ts:          twoDaysAgo.Add(16*time.Hour + 45*time.Minute),
		},
		// R10 (disk full, esc): charlie escalates
		{
			recordUID:   rUIDs[9],
			commentType: "esc",
			user:        "charlie.ops",
			message:     "Escalated to DBA team. WAL archive purge in progress — monitoring.",
			ts:          yesterday.Add(7*time.Hour + 55*time.Minute),
		},
		// R10 (disk full): alice follows up with a regular note
		{
			recordUID:   rUIDs[9],
			commentType: "",
			user:        "alice.martin",
			message:     "DBA confirms 15 GB freed. Still at 77%, continuing to watch.",
			ts:          yesterday.Add(10*time.Hour + 30*time.Minute),
		},
		// R11 (CPU, close): alice closes with post-mortem note
		{
			recordUID:   rUIDs[10],
			commentType: "close",
			user:        "alice.martin",
			message:     "False positive — scheduled nightly batch job. Added exception rule for 02:00–04:00 window.",
			ts:          twoDaysAgo.Add(16*time.Hour + 10*time.Minute),
		},
	}

	docs := make([]db.Document, 0, len(defs))
	for _, def := range defs {
		doc := db.Document{
			"uid":        uuid.NewString(),
			"record_uid": def.recordUID,
			"user":       def.user,
			"method":     auth.LocalMethod,
			"message":    def.message,
			"date":       def.ts.Format(time.RFC3339),
		}
		if def.commentType != "" {
			doc["type"] = def.commentType
		}
		docs = append(docs, doc)
	}

	_, err := drv.Write(ctx, "comment", docs, db.WriteOptions{
		Primary:    []string{"uid"},
		UpdateTime: true,
	})
	return err
}

// --- helpers ---

// demoBuildEnrichment applies the 3 demo rules to doc in place, computing the
// same field set the live pipeline would produce:
//
//   - "Parse Host Components": service, tier, instance_num via REGEX_PARSE
//   - "Day/Night Shift":       period via timestamp hour
//   - "Host and Message":      hash via MD5 (replicates aggregaterule.computeHash)
func demoBuildEnrichment(doc db.Document, ts time.Time) {
	if host, ok := doc["host"].(string); ok {
		if m := demoHostRE.FindStringSubmatch(host); m != nil {
			for i, name := range demoHostRE.SubexpNames() {
				switch name {
				case "service":
					doc["service"] = m[i]
				case "tier":
					doc["tier"] = m[i]
				case "instance_num":
					doc["instance_num"] = m[i]
				}
			}
		}
	}
	if h := ts.UTC().Hour(); h >= 8 && h < 20 {
		doc["period"] = "day"
	} else {
		doc["period"] = "night"
	}
	host, _ := doc["host"].(string)
	message, _ := doc["message"].(string)
	doc["hash"] = demoComputeHash(host, message)
	doc["plugins"] = []string{"rule", "aggregaterule"}
}

// demoComputeHash replicates aggregaterule.computeHash for the default "Host
// and Message" rule: MD5( name || "host=" || host || "|" || "message=" || msg || "|" ).
func demoComputeHash(host, message string) string {
	h := md5.New() //nolint:gosec
	h.Write([]byte("Host and Message"))
	h.Write([]byte("host"))
	h.Write([]byte("="))
	fmt.Fprint(h, host)
	h.Write([]byte("|"))
	h.Write([]byte("message"))
	h.Write([]byte("="))
	fmt.Fprint(h, message)
	h.Write([]byte("|"))
	return hex.EncodeToString(h.Sum(nil))
}

// demoLastSaturday returns last Saturday at 14:30 UTC (or today if today is
// Saturday). Used to stamp the staging-weekend snoozed alert.
func demoLastSaturday(now time.Time) time.Time {
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	wd := int(now.UTC().Weekday()) // 0=Sun … 6=Sat
	var daysAgo int
	if wd == 6 {
		daysAgo = 0
	} else {
		daysAgo = (wd + 1) % 7 // Sun→1, Mon→2, …, Fri→6
	}
	return today.AddDate(0, 0, -daysAgo).Add(14*time.Hour + 30*time.Minute)
}

// condAsMap serializes a condition.Cond to map[string]any via a JSON
// round-trip. The notification plugin's decodeEntry deserializes via the same
// path (json.Marshal → json.Unmarshal into Entry.Condition).
func condAsMap(c condition.Cond) map[string]any {
	raw, _ := json.Marshal(c)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return m
}
