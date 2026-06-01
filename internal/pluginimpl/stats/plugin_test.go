package stats

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	"github.com/snoozeweb/snooze/internal/config"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/db/sqlite"
	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/internal/telemetry"
)

type testHost struct{ drv *sqlite.Driver }

func newTestHost(t *testing.T) *testHost {
	t.Helper()
	path := filepath.Join(t.TempDir(), "snooze.db")
	drv, err := sqlite.New(context.Background(), sqlite.Config{Path: path})
	require.NoError(t, err)
	t.Cleanup(func() { _ = drv.Close() })
	return &testHost{drv: drv}
}

func (h *testHost) DB() db.Driver                { return h.drv }
func (h *testHost) Bus() plugins.Bus             { return nil }
func (h *testHost) Logger() *slog.Logger         { return slog.Default() }
func (h *testHost) Tracer() trace.Tracer         { return otel.Tracer("stats-test") }
func (h *testHost) Metrics() *telemetry.Registry { return telemetry.NewRegistry(nil) }
func (h *testHost) Config() *config.Config       { return config.Default() }
func (h *testHost) Plugin(string) plugins.Plugin { return nil }

func TestRegistration(t *testing.T) {
	require.True(t, slices.Contains(plugins.Registered(), "stats"))
}

func TestPostInitRoundtrip(t *testing.T) {
	host := newTestHost(t)
	p := &Plugin{meta: plugins.Metadata{Name: "stats"}}
	require.NoError(t, p.PostInit(context.Background(), host))
	require.NoError(t, p.Reload(context.Background()))
	require.Equal(t, "stats", p.Name())
}

func TestValidate(t *testing.T) {
	p := &Plugin{}
	require.NoError(t, p.Validate(nil))
	require.NoError(t, p.Validate(map[string]any{"key": "x", "value": 1.0}))
}

// callStatsHandler is a helper that calls the handleStats handler with
// the given from/to RFC3339 strings and returns the decoded response.
func callStatsHandler(t *testing.T, host plugins.Host, from, to string) statsResponse {
	t.Helper()
	p := &Plugin{meta: plugins.Metadata{Name: "stats"}, host: host}
	req := httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/stats?from=%s&to=%s&bucket=3600", from, to), nil)
	w := httptest.NewRecorder()
	p.handleStats(host)(w, req)
	require.Equal(t, http.StatusOK, w.Code, "handler returned non-200: %s", w.Body.String())
	var resp statsResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	return resp
}

// TestHandleStats_CounterSeriesAndTotals seeds the stats counter collection
// and a record, then asserts the new /stats response shape:
//   - Series sums the "Alerts" key from alert_hit/source docs
//   - Totals.ByActionFailure is populated from action_error docs
//   - Totals.BySeverity is populated from alert_hit/severity docs
//   - Totals.ByHost is populated from alert_hit/host docs
//   - Snapshot.ByState reflects a seeded record
//   - Weekday has a nonzero entry for the bucket's weekday
func TestHandleStats_CounterSeriesAndTotals(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	host := newTestHost(t)

	// A fixed hour-aligned bucket inside our query window.
	// 2024-01-15 10:00:00 UTC (Monday = weekday 1)
	const bucket = int64(1705312800) // 2024-01-15T10:00:00Z
	wantWeekday := fmt.Sprintf("%d", int(time.Unix(bucket, 0).UTC().Weekday()))

	from := time.Unix(bucket-3600, 0).UTC().Format(time.RFC3339)
	to := time.Unix(bucket+7200, 0).UTC().Format(time.RFC3339)

	// Seed stats counter documents.
	statsDocs := []db.Document{
		// alert_hit dim=source → contributes to "Alerts" series and TotalHits
		{"metric": "alert_hit", "dim": "source", "key": "syslog", "bucket": bucket, "value": int64(5)},
		// alert_hit dim=severity → contributes to BySeverity
		{"metric": "alert_hit", "dim": "severity", "key": "critical", "bucket": bucket, "value": int64(3)},
		// alert_hit dim=environment → contributes to ByEnvironment
		{"metric": "alert_hit", "dim": "environment", "key": "prod", "bucket": bucket, "value": int64(2)},
		// alert_hit dim=host → contributes to ByHost
		{"metric": "alert_hit", "dim": "host", "key": "h1", "bucket": bucket, "value": int64(4)},
		// alert_throttled → contributes to ByThrottled
		{"metric": "alert_throttled", "dim": "name", "key": "MyRule", "bucket": bucket, "value": int64(1)},
		// alert_snoozed → contributes to BySnoozed
		{"metric": "alert_snoozed", "dim": "name", "key": "MySnoozePol", "bucket": bucket, "value": int64(2)},
		// notification_sent → contributes to ByNotification
		{"metric": "notification_sent", "dim": "channel", "key": "slack", "bucket": bucket, "value": int64(3)},
		// action_success → contributes to ByActionSuccess
		{"metric": "action_success", "dim": "action", "key": "Email", "bucket": bucket, "value": int64(6)},
		// action_error → contributes to ByActionFailure
		{"metric": "action_error", "dim": "action", "key": "Jira", "bucket": bucket, "value": int64(7)},
	}
	_, err := host.DB().Write(ctx, plugins.StatsCollection, statsDocs, db.WriteOptions{
		Primary: []string{"metric", "dim", "key", "bucket"},
	})
	require.NoError(t, err)

	// Seed a record so ByState is populated via the record aggregation path.
	_, err = host.DB().Write(ctx, "record", []db.Document{
		{
			"state":      "open",
			"severity":   "critical",
			"source":     "syslog",
			"date_epoch": float64(bucket),
		},
	}, db.WriteOptions{})
	require.NoError(t, err)

	resp := callStatsHandler(t, host, from, to)

	// ── Series ──────────────────────────────────────────────────────────────
	// The series must have a nonzero "Alerts" count at the bucket slot.
	var alertsTotal, throttledTotal, snoozedTotal, notifTotal, actionErrTotal int
	for _, sb := range resp.Data.Series {
		alertsTotal += sb.Counts["Alerts"]
		throttledTotal += sb.Counts["Throttled"]
		snoozedTotal += sb.Counts["Snoozed"]
		notifTotal += sb.Counts["Notification sent"]
		actionErrTotal += sb.Counts["Action error"]
	}
	require.Equal(t, 5, alertsTotal, "sum of Alerts across series should be 5")
	require.Equal(t, 1, throttledTotal, "sum of Throttled across series should be 1")
	require.Equal(t, 2, snoozedTotal, "sum of Snoozed across series should be 2")
	require.Equal(t, 3, notifTotal, "sum of Notification sent across series should be 3")
	require.Equal(t, 7, actionErrTotal, "sum of Action error across series should be 7")

	// ── Totals ──────────────────────────────────────────────────────────────
	totals := resp.Data.Totals
	require.Equal(t, map[string]int{"critical": 3}, totals.BySeverity, "BySeverity mismatch")
	require.Equal(t, map[string]int{"prod": 2}, totals.ByEnvironment, "ByEnvironment mismatch")
	require.Equal(t, map[string]int{"h1": 4}, totals.ByHost, "ByHost mismatch")
	require.Equal(t, map[string]int{"MyRule": 1}, totals.ByThrottled, "ByThrottled mismatch")
	require.Equal(t, map[string]int{"MySnoozePol": 2}, totals.BySnoozed, "BySnoozed mismatch")
	require.Equal(t, map[string]int{"slack": 3}, totals.ByNotification, "ByNotification mismatch")
	require.Equal(t, map[string]int{"Email": 6}, totals.ByActionSuccess, "ByActionSuccess mismatch")
	require.Equal(t, map[string]int{"Jira": 7}, totals.ByActionFailure, "ByActionFailure mismatch")

	// ── Snapshot ─────────────────────────────────────────────────────────────
	snap := resp.Data.Snapshot
	require.Equal(t, 1, snap.ByState["open"], "snapshot.by_state[open] from seeded record")
	require.Equal(t, 1, snap.Open, "snapshot.open KPI")
	require.Equal(t, 0, snap.Ack, "snapshot.ack KPI")
	require.Equal(t, 0, snap.Closed, "snapshot.closed KPI")
	require.Equal(t, 5, snap.TotalHits, "snapshot.total_hits should sum Alerts series")

	// ── Weekday ──────────────────────────────────────────────────────────────
	require.NotEmpty(t, resp.Data.Weekday, "weekday map should be non-empty")
	require.Equal(t, 5, resp.Data.Weekday[wantWeekday],
		"weekday[%s] should be 5 (from alert_hit/source doc)", wantWeekday)
}

// TestHandleStats_TopHostCapping verifies that ByHost is capped to the top-10
// hosts by hit count.
func TestHandleStats_TopHostCapping(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	host := newTestHost(t)

	const bucket = int64(1705312800)
	from := time.Unix(bucket-3600, 0).UTC().Format(time.RFC3339)
	to := time.Unix(bucket+3600, 0).UTC().Format(time.RFC3339)

	// Seed 12 distinct hosts — only top 10 should survive.
	docs := make([]db.Document, 12)
	for i := range docs {
		docs[i] = db.Document{
			"metric": "alert_hit",
			"dim":    "host",
			"key":    fmt.Sprintf("host%02d", i),
			"bucket": bucket,
			"value":  int64(i + 1), // host11 has highest count
		}
	}
	_, err := host.DB().Write(ctx, plugins.StatsCollection, docs, db.WriteOptions{
		Primary: []string{"metric", "dim", "key", "bucket"},
	})
	require.NoError(t, err)

	resp := callStatsHandler(t, host, from, to)

	require.LessOrEqual(t, len(resp.Data.Totals.ByHost), 10,
		"ByHost should be capped at 10 entries; got %d", len(resp.Data.Totals.ByHost))
	// host00 (value=1) should be excluded; host11 (value=12) should be present.
	_, hasLowest := resp.Data.Totals.ByHost["host00"]
	_, hasHighest := resp.Data.Totals.ByHost["host11"]
	require.False(t, hasLowest, "lowest-count host should be evicted")
	require.True(t, hasHighest, "highest-count host should be retained")
}
