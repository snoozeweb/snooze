// Package stats implements the "stats" data-model plugin and serves the
// dashboard's aggregated time-series at GET /api/v1/stats.
//
// The response is composed from two sources:
//  1. The `stats` counter collection (written by the alert/action pipeline via
//     plugins.RecordStat) — provides the time-series, per-window totals, and
//     weekday distribution.
//  2. The `record` collection, aggregated either via the optional
//     db.RecordAggregator SQL path or the in-Go fallback — provides the
//     ByState snapshot and its derived KPIs.
package stats

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/plugins"
)

//go:embed metadata.yaml
var metaYAML []byte

func init() {
	plugins.Register("stats", metaYAML, factory)
}

func factory(meta plugins.Metadata) (plugins.Plugin, error) {
	return &Plugin{meta: meta}, nil
}

// Plugin is the data-model plugin for stored stats documents.
type Plugin struct {
	meta plugins.Metadata
	host plugins.Host
}

// Name returns the registered plugin name and collection identifier.
func (p *Plugin) Name() string { return "stats" }

// Metadata returns the parsed metadata.yaml descriptor.
func (p *Plugin) Metadata() plugins.Metadata { return p.meta }

// PostInit captures the host for subsequent calls.
func (p *Plugin) PostInit(_ context.Context, host plugins.Host) error {
	p.host = host
	return nil
}

// Reload is a no-op: stats are aggregated on the fly.
func (p *Plugin) Reload(_ context.Context) error { return nil }

// Schema returns the JSON Schema for a stats counter document.
func (p *Plugin) Schema() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"key":        map[string]any{"type": "string"},
			"value":      map[string]any{"type": "number"},
			"date_epoch": map[string]any{"type": "number"},
		},
		"additionalProperties": true,
	}
}

// Validate accepts any well-formed map.
func (p *Plugin) Validate(_ map[string]any) error { return nil }

// RegisterRoutes mounts GET /api/v1/stats. The generic CRUD endpoints are
// intentionally skipped: the stats collection is no longer user-writable
// from the UI.
func (p *Plugin) RegisterRoutes(r chi.Router, host plugins.Host) {
	r.Get("/", p.handleStats(host))
}

type statsResponse struct {
	Data statsData `json:"data"`
	Meta statsMeta `json:"meta"`
}

type statsData struct {
	Series   []seriesBucket `json:"series"`
	Totals   statsTotals    `json:"totals"`
	Snapshot statsSnapshot  `json:"snapshot"`
	Weekday  map[string]int `json:"weekday"`
}

type seriesBucket struct {
	T      string         `json:"t"`
	Counts map[string]int `json:"counts"`
}

type statsTotals struct {
	BySeverity      map[string]int `json:"by_severity"`
	ByEnvironment   map[string]int `json:"by_environment"`
	ByHost          map[string]int `json:"by_host"`
	ByActionSuccess map[string]int `json:"by_action_success"`
	ByActionFailure map[string]int `json:"by_action_failure"`
	ByThrottled     map[string]int `json:"by_throttled"`
	BySnoozed       map[string]int `json:"by_snoozed"`
	ByNotification  map[string]int `json:"by_notification"`
}

type statsSnapshot struct {
	ByState   map[string]int `json:"by_state"`
	TotalHits int            `json:"total_hits"`
	Open      int            `json:"open"`
	Ack       int            `json:"ack"`
	Closed    int            `json:"closed"` // domain key is "close"; public JSON field is "closed"
}

type statsMeta struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Bucket int    `json:"bucket"`
}

// handleStats serves the dashboard's aggregate. It composes data from two
// sources: the stats counter collection (series, totals, weekday) and the
// record collection (snapshot/ByState). For the record path, drivers
// implementing the optional db.RecordAggregator capability (today: SQLite)
// answer in SQL; others fall back to fetching records and reducing in Go.
func (p *Plugin) handleStats(host plugins.Host) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		from, err := parseTime(q.Get("from"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"code": "bad_request", "detail": "bad `from`: " + err.Error(),
			})
			return
		}
		to, err := parseTime(q.Get("to"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"code": "bad_request", "detail": "bad `to`: " + err.Error(),
			})
			return
		}
		bucketSec, err := strconv.Atoi(q.Get("bucket"))
		if err != nil || bucketSec <= 0 {
			bucketSec = 3600
		}
		stride := int64(bucketSec)
		fromEpoch, toEpoch := from.Unix(), to.Unix()

		// ── 1. Record snapshot (ByState KPIs only) ───────────────────────────
		var byState map[string]int64

		if agg, ok := host.DB().(db.RecordAggregator); ok {
			res, aggErr := agg.RecordStats(r.Context(), from, to, stride)
			if aggErr != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]any{
					"code": "db_error", "detail": aggErr.Error(),
				})
				return
			}
			byState = res.ByState
		} else {
			_, _, _, st, scanErr := reduceInGo(r.Context(), host, fromEpoch, toEpoch, stride)
			if scanErr != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]any{
					"code": "db_error", "detail": scanErr.Error(),
				})
				return
			}
			byState = st
		}

		// ── 2. Stats counter collection ───────────────────────────────────────
		// Counter docs are always written on hourly buckets (RecordStat truncates to
		// the hour), so fromB aligns the lower bound to the hour. The series loop
		// below starts from this same aligned bound so every counted doc lands in an
		// emitted slot — keeping the series consistent with the totals for any stride.
		fromB := (fromEpoch / 3600) * 3600
		cond := condition.And(
			condition.Cond{Op: condition.OpGte, Field: "bucket", Value: fromB},
			condition.Cond{Op: condition.OpLte, Field: "bucket", Value: toEpoch},
		)
		docs, total, dbErr := host.DB().Search(r.Context(), plugins.StatsCollection, cond, db.Page{
			PerPage: 100000,
			OrderBy: "bucket",
			Asc:     true,
		})
		if dbErr != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{
				"code": "db_error", "detail": dbErr.Error(),
			})
			return
		}
		if len(docs) >= 100000 && total > len(docs) {
			if lg := host.Logger(); lg != nil {
				lg.WarnContext(r.Context(), "stats: counter docs truncated; dashboard totals may under-count",
					"fetched", len(docs), "total", total)
			}
		}

		// ── 3. Reduce counter docs in Go ──────────────────────────────────────
		// seriesMap: bucket-aligned epoch → series key → count
		seriesMap := map[int64]map[string]int{}

		totals := statsTotals{
			BySeverity:      map[string]int{},
			ByEnvironment:   map[string]int{},
			ByHost:          map[string]int{},
			ByActionSuccess: map[string]int{},
			ByActionFailure: map[string]int{},
			ByThrottled:     map[string]int{},
			BySnoozed:       map[string]int{},
			ByNotification:  map[string]int{},
		}
		weekday := map[string]int{}
		totalHits := 0

		for _, doc := range docs {
			metric, _ := doc["metric"].(string)
			dim, _ := doc["dim"].(string)
			key, _ := doc["key"].(string)
			bktRaw := asInt64(doc["bucket"])
			val := int(asInt64(doc["value"]))

			// Series key mapping: (metric, dim) → legend label.
			// alert_hit+dim=="source" is the canonical count: alert_hit is recorded
			// once per dimension, so picking a single dim avoids 4× inflation across
			// severity/environment/host/source; source is always non-empty.
			seriesKey := ""
			switch metric {
			case "alert_hit":
				if dim == "source" {
					seriesKey = "Alerts"
				}
			case "alert_throttled":
				seriesKey = "Throttled"
			case "alert_snoozed":
				seriesKey = "Snoozed"
			case "notification_sent":
				seriesKey = "Notification sent"
			case "action_error":
				seriesKey = "Action error"
			}
			if seriesKey != "" {
				slot := (bktRaw / stride) * stride
				if seriesMap[slot] == nil {
					seriesMap[slot] = map[string]int{}
				}
				seriesMap[slot][seriesKey] += val
			}

			// Totals mapping
			switch metric {
			case "alert_hit":
				switch dim {
				case "severity":
					totals.BySeverity[key] += val
				case "environment":
					totals.ByEnvironment[key] += val
				case "host":
					totals.ByHost[key] += val
				case "source":
					// TotalHits and weekday (below)
					totalHits += val
					wd := strconv.Itoa(int(time.Unix(bktRaw, 0).UTC().Weekday()))
					weekday[wd] += val
				}
			case "alert_throttled":
				totals.ByThrottled[key] += val
			case "alert_snoozed":
				totals.BySnoozed[key] += val
			case "notification_sent":
				totals.ByNotification[key] += val
			case "action_success":
				totals.ByActionSuccess[key] += val
			case "action_error":
				totals.ByActionFailure[key] += val
			}
		}

		// Cap ByHost to top-10.
		totals.ByHost = topN(totals.ByHost, 10)

		// ── 4. Emit continuous series (no gap-jumps) ──────────────────────────
		series := make([]seriesBucket, 0)
		for t := (fromB / stride) * stride; t <= toEpoch; t += stride {
			counts := seriesMap[t]
			out := make(map[string]int, len(counts))
			for k, v := range counts {
				out[k] = v
			}
			series = append(series, seriesBucket{
				T:      time.Unix(t, 0).UTC().Format(time.RFC3339),
				Counts: out,
			})
		}

		// ── 5. Assemble snapshot ──────────────────────────────────────────────
		byStateInt := toIntMap(byState)
		snapshot := statsSnapshot{
			ByState:   byStateInt,
			TotalHits: totalHits,
			Open:      byStateInt["open"],
			Ack:       byStateInt["ack"],
			Closed:    byStateInt["close"],
		}

		writeJSON(w, http.StatusOK, statsResponse{
			Data: statsData{
				Series:   series,
				Totals:   totals,
				Snapshot: snapshot,
				Weekday:  weekday,
			},
			Meta: statsMeta{
				From:   from.UTC().Format(time.RFC3339),
				To:     to.UTC().Format(time.RFC3339),
				Bucket: bucketSec,
			},
		})
	}
}

// asInt64 coerces common numeric types that come back from JSON-stored backends
// (where all numbers decode as float64) or typed backends (int64/int) into
// int64. Unknown types return 0.
func asInt64(v any) int64 {
	switch x := v.(type) {
	case float64:
		return int64(x)
	case int64:
		return x
	case int:
		return int64(x)
	case int32:
		return int64(x)
	case json.Number:
		n, _ := x.Int64()
		return n
	}
	return 0
}

// topN returns a new map containing only the n entries with the highest values.
// If len(m) <= n the original map is returned unchanged.
func topN(m map[string]int, n int) map[string]int {
	if len(m) <= n {
		return m
	}
	type kv struct {
		k string
		v int
	}
	arr := make([]kv, 0, len(m))
	for k, v := range m {
		arr = append(arr, kv{k, v})
	}
	sort.Slice(arr, func(i, j int) bool {
		if arr[i].v != arr[j].v {
			return arr[i].v > arr[j].v
		}
		return arr[i].k < arr[j].k
	})
	out := make(map[string]int, n)
	for _, e := range arr[:n] {
		out[e.k] = e.v
	}
	return out
}

func toIntMap(in map[string]int64) map[string]int {
	out := make(map[string]int, len(in))
	for k, v := range in {
		out[k] = int(v)
	}
	return out
}

// reduceInGo is the fallback for drivers that don't implement
// db.RecordAggregator: pull up to 10k records and aggregate locally.
// NOTE: The bucketed and bySeverity/byEnvironment returns are preserved for
// signature compatibility but are no longer surfaced in the response — the
// counter collection provides those dimensions now. Only byState is used.
func reduceInGo(ctx context.Context, host plugins.Host, fromEpoch, toEpoch, stride int64) (
	bucketed map[int64]map[string]int64,
	bySeverity, byEnvironment, byState map[string]int64,
	err error,
) {
	bucketed = map[int64]map[string]int64{}
	bySeverity = map[string]int64{}
	byEnvironment = map[string]int64{}
	byState = map[string]int64{}

	records, _, err := host.DB().Search(ctx, "record", condition.Cond{}, db.Page{
		PerPage: 10000,
		OrderBy: "date_epoch",
		Asc:     true,
	})
	if err != nil {
		return nil, nil, nil, nil, err
	}
	for _, rec := range records {
		epoch, ok := recordEpoch(rec)
		if !ok || epoch < fromEpoch || epoch > toEpoch {
			continue
		}
		sev, _ := rec["severity"].(string)
		if sev == "" {
			sev = "info"
		}
		bySeverity[sev]++
		env, _ := rec["environment"].(string)
		if env == "" {
			env = "(none)"
		}
		byEnvironment[env]++
		st, _ := rec["state"].(string)
		if st == "" {
			st = "open"
		}
		byState[st]++

		source, _ := rec["source"].(string)
		if source == "" {
			source = "unknown"
		}
		slot := (epoch / stride) * stride
		row := bucketed[slot]
		if row == nil {
			row = map[string]int64{}
			bucketed[slot] = row
		}
		row[source]++
	}
	return bucketed, bySeverity, byEnvironment, byState, nil
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func parseTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, fmt.Errorf("missing")
	}
	// Try RFC3339 first (what the UI sends), then a Unix epoch fallback.
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return time.Unix(n, 0).UTC(), nil
	}
	return time.Time{}, fmt.Errorf("unrecognised time %q", s)
}

// recordEpoch returns the record's date in seconds since the epoch, derived
// from `date_epoch` (number) or `date` (RFC3339 string). Records missing
// both fields are skipped.
func recordEpoch(rec db.Document) (int64, bool) {
	switch v := rec["date_epoch"].(type) {
	case float64:
		return int64(v), true
	case int64:
		return v, true
	case int:
		return int64(v), true
	}
	if s, ok := rec["date"].(string); ok && s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t.Unix(), true
		}
	}
	return 0, false
}
