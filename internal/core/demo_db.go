package core

import (
	"context"
	"crypto/md5" //nolint:gosec
	"encoding/hex"
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

// statsCollection mirrors plugins.StatsCollection without importing the package.
const demoStatsCollection = "stats"

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
	if err := seedDemoStats(ctx, drv, now); err != nil {
		return fmt.Errorf("demo_db: stats: %w", err)
	}

	if _, err := drv.Write(ctx, generalCollection, []db.Document{{demoMarkerField: true}},
		db.WriteOptions{UpdateTime: true}); err != nil {
		return fmt.Errorf("demo_db: write marker: %w", err)
	}
	return nil
}

// --- environments ---

func seedDemoEnvironments(ctx context.Context, drv db.Driver) error {
	// Conditions use the frontend's discriminated-union shape {type, field, value}
	// so the condition editor renders them correctly when viewing the environment.
	eq := func(field, value string) map[string]any {
		return map[string]any{"type": "EQUALS", "field": field, "value": value}
	}
	envs := []db.Document{
		{
			"name":       "production",
			"color":      "#e53935",
			"condition":  eq("environment", "production"),
			"tree_order": 1,
		},
		{
			"name":       "staging",
			"color":      "#fb8c00",
			"condition":  eq("environment", "staging"),
			"tree_order": 2,
		},
		{
			"name":       "development",
			"color":      "#43a047",
			"condition":  eq("environment", "development"),
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
	// Conditions use frontend shape so the condition editor shows them correctly.
	matches := func(field, regex string) map[string]any {
		return map[string]any{"type": "MATCHES", "field": field, "value": regex}
	}
	rules := []db.Document{
		{
			// Mirrors the "Parse Host Components" rule: REGEX_PARSE on host
			// sets service, tier, instance_num on every passing alert.
			"name":    "Parse Host Components",
			"enabled": true,
			"condition": map[string]any{"type": "ALWAYS_TRUE"},
			"modifications": []any{
				[]any{"REGEX_PARSE", "host", `^(?P<service>[a-z]+)-(?P<tier>[a-z]+)-(?P<instance_num>\d+)`},
			},
			"tree_order": 1,
		},
		{
			// Day Shift: hours 08–19.
			"name":    "Day Shift",
			"enabled": true,
			"condition": matches("timestamp", ` (0[89]|1[0-9]):`),
			"modifications": []any{
				[]any{"SET", "period", "day"},
			},
			"tree_order": 2,
		},
		{
			// Night Shift: hours 00–07 and 20–23.
			"name":    "Night Shift",
			"enabled": true,
			"condition": matches("timestamp", ` (0[0-7]|2[0-3]):`),
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
	// Conditions use frontend shape ({type, field, value}) so the condition
	// editor renders them correctly when viewing / editing a notification.
	eq := func(field, value string) map[string]any {
		return map[string]any{"type": "EQUALS", "field": field, "value": value}
	}
	and := func(children ...map[string]any) map[string]any {
		args := make([]any, len(children))
		for i, c := range children {
			args[i] = c
		}
		return map[string]any{"type": "AND", "args": args}
	}
	notifications := []db.Document{
		{
			"name":      "Critical Alerts",
			"enabled":   true,
			"condition": eq("severity", "critical"),
			"actions":   []string{"Slack #ops-alerts"},
		},
		{
			"name":      "Production Incidents",
			"enabled":   true,
			"condition": and(eq("environment", "production"), eq("severity", "critical")),
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
	// Conditions use frontend shape so the condition editor renders them correctly.
	eq := func(field, value string) map[string]any {
		return map[string]any{"type": "EQUALS", "field": field, "value": value}
	}
	and := func(children ...map[string]any) map[string]any {
		args := make([]any, len(children))
		for i, c := range children {
			args[i] = c
		}
		return map[string]any{"type": "AND", "args": args}
	}

	snoozes := []db.Document{
		{
			"name":      "Night Warning Suppression",
			"enabled":   true,
			"discard":   false,
			"condition": eq("severity", "warning"),
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
			"condition":    and(eq("environment", "development"), eq("severity", "info")),
			"hits_enabled": true,
			"hits":         int64(0),
		},
		{
			"name":    "Staging Weekend Maintenance",
			"enabled": true,
			"discard": false,
			"condition": eq("environment", "staging"),
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
//
// Records are written via ReplaceOne (upsert) so that pre-assigned UIDs are
// honoured on first boot — Write rejects documents whose uid does not already
// exist in the collection, which would silently drop all records.
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
		// ReplaceOne upserts by uid so pre-assigned UIDs work on first boot.
		// Write rejects documents whose uid doesn't already exist, which would
		// silently drop every record.
		if _, err := drv.ReplaceOne(ctx, "record", db.Document{"uid": rUIDs[i]}, doc, true); err != nil {
			return fmt.Errorf("record %d: %w", i, err)
		}
	}
	return nil
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

	for _, def := range defs {
		uid := uuid.NewString()
		doc := db.Document{
			"uid":        uid,
			"record_uid": def.recordUID,
			"user":       def.user,
			"method":     auth.LocalMethod,
			"message":    def.message,
			"date":       def.ts.Format(time.RFC3339),
		}
		if def.commentType != "" {
			doc["type"] = def.commentType
		}
		// ReplaceOne upserts by uid — same reason as records above.
		if _, err := drv.ReplaceOne(ctx, "comment", db.Document{"uid": uid}, doc, false); err != nil {
			return fmt.Errorf("comment: %w", err)
		}
	}
	return nil
}

// --- stats (dashboard time-series) ---

// seedDemoStats populates the `stats` counter collection with 14 days of
// hourly alert metrics so the dashboard renders non-empty charts on first
// visit. The data mirrors what RecordStat / the alert pipeline would have
// written for a realistic workload: alert_hit (by severity / environment /
// source), alert_snoozed, and notification_sent.
//
// Stats docs are keyed on (metric, dim, key, bucket); Write with those
// as Primary fields upserts correctly because the docs carry no uid.
func seedDemoStats(ctx context.Context, drv db.Driver, now time.Time) error {
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	var docs []db.Document

	for dayOffset := 1; dayOffset <= 14; dayOffset++ {
		day := today.AddDate(0, 0, -dayOffset)
		isWeekend := day.Weekday() == time.Saturday || day.Weekday() == time.Sunday

		for hour := 0; hour < 24; hour++ {
			bucket := day.Add(time.Duration(hour) * time.Hour).Unix()
			base := demoHourlyBase(hour)
			if isWeekend {
				base = base * 3 / 10 // weekends are quieter
			}
			if base == 0 {
				continue
			}

			// Severity split: 20% critical, 50% warning, 30% info
			crit := max64(1, base*2/10)
			warn := max64(1, base*5/10)
			info := max64(1, base*3/10)

			docs = append(docs,
				demoStatDoc("alert_hit", "severity", "critical", bucket, crit),
				demoStatDoc("alert_hit", "severity", "warning", bucket, warn),
				demoStatDoc("alert_hit", "severity", "info", bucket, info),
			)

			// Environment split: 40% prod, 35% staging, 25% dev
			prod := max64(1, base*4/10)
			stag := max64(1, base*35/100)
			dev := max64(1, base*25/100)

			docs = append(docs,
				demoStatDoc("alert_hit", "environment", "production", bucket, prod),
				demoStatDoc("alert_hit", "environment", "staging", bucket, stag),
				demoStatDoc("alert_hit", "environment", "development", bucket, dev),
			)

			// Source split (drives the Alerts series line): 60% prometheus,
			// 30% alertmanager, 10% grafana.
			prom := max64(1, base*6/10)
			am := max64(1, base*3/10)
			graf := max64(1, base*1/10)

			docs = append(docs,
				demoStatDoc("alert_hit", "source", "prometheus", bucket, prom),
				demoStatDoc("alert_hit", "source", "alertmanager", bucket, am),
				demoStatDoc("alert_hit", "source", "grafana", bucket, graf),
			)

			// Snoozed: ~30% of warnings are suppressed at night (hours 20–7)
			if hour >= 20 || hour < 8 {
				snoozed := max64(1, warn*3/10)
				docs = append(docs,
					demoStatDoc("alert_snoozed", "name", "Night Warning Suppression", bucket, snoozed),
				)
			}

			// Dev info alerts are always snoozed by Dev Noise Reduction
			docs = append(docs,
				demoStatDoc("alert_snoozed", "name", "Dev Noise Reduction", bucket, info*25/100+1),
			)

			// Notifications sent for critical alerts
			if crit > 0 {
				docs = append(docs,
					demoStatDoc("notification_sent", "name", "Critical Alerts", bucket, crit),
					demoStatDoc("notification_sent", "name", "Production Incidents", bucket, max64(1, crit*4/10)),
				)
			}
		}
	}

	if len(docs) == 0 {
		return nil
	}
	_, err := drv.Write(ctx, demoStatsCollection, docs, db.WriteOptions{
		Primary:    []string{"metric", "dim", "key", "bucket"},
		UpdateTime: false,
	})
	return err
}

// demoStatDoc builds one stats counter document.
func demoStatDoc(metric, dim, key string, bucket, value int64) db.Document {
	return db.Document{
		"metric": metric,
		"dim":    dim,
		"key":    key,
		"bucket": bucket,
		"value":  float64(value),
	}
}

// demoHourlyBase returns the expected alert count for a given UTC hour,
// modelling a business-hours traffic pattern (peak midday, quiet nights).
func demoHourlyBase(hour int) int64 {
	switch {
	case hour < 6:
		return 0
	case hour < 8:
		return 2
	case hour < 10:
		return int64(3 + hour - 8) // 3, 4
	case hour < 13:
		return int64(5 + hour - 10) // 5, 6, 7
	case hour == 13:
		return 8 // peak
	case hour < 18:
		return int64(8 - (hour - 13)) // 7, 6, 5, 4
	case hour < 20:
		return 3
	default:
		return 1
	}
}

// max64 returns the larger of a and b.
func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
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
