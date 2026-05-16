// Package stats implements the "stats" data-model plugin and serves the
// dashboard's aggregated time-series at GET /api/v1/stats.
//
// The Python era kept a precomputed `stats` collection and surfaced it via
// a custom StatsRoute. The Go port instead aggregates the record collection
// on demand: at typical alert volumes the cost is trivial and we avoid the
// counter-pipeline complexity. The plugin still owns a stats collection
// schema for backward compatibility, but no rows are read from it today.
package stats

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
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
	Series []seriesBucket `json:"series"`
	Totals statsTotals    `json:"totals"`
}

type seriesBucket struct {
	T      string         `json:"t"`
	Counts map[string]int `json:"counts"`
}

type statsTotals struct {
	BySeverity      map[string]int `json:"by_severity"`
	ByEnvironment   map[string]int `json:"by_environment"`
	ByActionSuccess map[string]int `json:"by_action_success"`
	ByActionFailure map[string]int `json:"by_action_failure"`
}

type statsMeta struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Bucket int    `json:"bucket"`
}

// handleStats serves the dashboard's aggregate. Drivers implementing the
// optional db.RecordAggregator capability (today: SQLite) answer in SQL;
// others fall back to fetching records and reducing in Go.
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

		var bucketed map[int64]map[string]int64
		var bySeverity, byEnvironment map[string]int64

		if agg, ok := host.DB().(db.RecordAggregator); ok {
			res, aggErr := agg.RecordStats(r.Context(), from, to, stride)
			if aggErr != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]any{
					"code": "db_error", "detail": aggErr.Error(),
				})
				return
			}
			bucketed = res.Series
			bySeverity = res.BySeverity
			byEnvironment = res.ByEnvironment
		} else {
			b, sev, env, scanErr := reduceInGo(r.Context(), host, fromEpoch, toEpoch, stride)
			if scanErr != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]any{
					"code": "db_error", "detail": scanErr.Error(),
				})
				return
			}
			bucketed, bySeverity, byEnvironment = b, sev, env
		}

		// Emit a continuous series so the line chart doesn't gap-jump.
		series := make([]seriesBucket, 0)
		for t := (fromEpoch / stride) * stride; t <= toEpoch; t += stride {
			counts := bucketed[t]
			out := make(map[string]int, len(counts))
			for k, v := range counts {
				out[k] = int(v)
			}
			series = append(series, seriesBucket{
				T:      time.Unix(t, 0).UTC().Format(time.RFC3339),
				Counts: out,
			})
		}

		writeJSON(w, http.StatusOK, statsResponse{
			Data: statsData{
				Series: series,
				Totals: statsTotals{
					BySeverity:      toIntMap(bySeverity),
					ByEnvironment:   toIntMap(byEnvironment),
					ByActionSuccess: map[string]int{},
					ByActionFailure: map[string]int{},
				},
			},
			Meta: statsMeta{
				From:   from.UTC().Format(time.RFC3339),
				To:     to.UTC().Format(time.RFC3339),
				Bucket: bucketSec,
			},
		})
	}
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
func reduceInGo(ctx context.Context, host plugins.Host, fromEpoch, toEpoch, stride int64) (
	bucketed map[int64]map[string]int64,
	bySeverity, byEnvironment map[string]int64,
	err error,
) {
	bucketed = map[int64]map[string]int64{}
	bySeverity = map[string]int64{}
	byEnvironment = map[string]int64{}

	records, _, err := host.DB().Search(ctx, "record", condition.Cond{}, db.Page{
		PerPage: 10000,
		OrderBy: "date_epoch",
		Asc:     true,
	})
	if err != nil {
		return nil, nil, nil, err
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
	return bucketed, bySeverity, byEnvironment, nil
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
