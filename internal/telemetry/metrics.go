package telemetry

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

// Registry bundles every prometheus metric Snooze emits.
//
// The struct is allocated once per process by NewRegistry and reused by every
// goroutine. Counter/Histogram methods on prometheus types are concurrency-safe.
type Registry struct {
	// AlertHit counts processed alerts, labelled by the terminal plugin action.
	// Labels: plugin, action (continue|abort|abort_write|abort_update).
	AlertHit *prometheus.CounterVec

	// ProcessAlertDuration measures the end-to-end pipeline latency, labelled
	// by the pipeline stage.
	ProcessAlertDuration *prometheus.HistogramVec

	// PluginDuration measures per-plugin processing latency.
	// Labels: plugin, method (process|notify|webhook).
	PluginDuration *prometheus.HistogramVec

	// SupervisorPanic counts goroutine panics caught by the Supervisor.
	// Labels: worker.
	SupervisorPanic *prometheus.CounterVec

	// HTTPRequestDuration measures HTTP request latency.
	// Labels: method, route, status.
	HTTPRequestDuration *prometheus.HistogramVec

	// DBQueryDuration measures database driver call latency.
	// Labels: driver, op, collection.
	DBQueryDuration *prometheus.HistogramVec
}

// NewRegistry constructs the canonical Snooze metric set and registers it,
// alongside the default Go runtime + process collectors, on reg.
//
// Pass a fresh *prometheus.Registry in tests to keep them isolated.
func NewRegistry(reg prometheus.Registerer) *Registry {
	r := &Registry{
		AlertHit: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "snooze_alert_hit_total",
			Help: "Number of processed alerts by plugin and final pipeline action.",
		}, []string{"plugin", "action"}),
		ProcessAlertDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "snooze_process_alert_duration_seconds",
			Help:    "Latency of alert pipeline stages.",
			Buckets: prometheus.DefBuckets,
		}, []string{"stage"}),
		PluginDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "snooze_plugin_duration_seconds",
			Help:    "Latency of plugin method calls.",
			Buckets: prometheus.DefBuckets,
		}, []string{"plugin", "method"}),
		SupervisorPanic: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "snooze_supervisor_panic_total",
			Help: "Number of panics recovered by the supervisor.",
		}, []string{"worker"}),
		HTTPRequestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "snooze_http_request_duration_seconds",
			Help:    "HTTP server-side request latency.",
			Buckets: prometheus.DefBuckets,
		}, []string{"method", "route", "status"}),
		DBQueryDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "snooze_db_query_duration_seconds",
			Help:    "Database driver call latency.",
			Buckets: prometheus.DefBuckets,
		}, []string{"driver", "op", "collection"}),
	}
	if reg != nil {
		reg.MustRegister(
			r.AlertHit,
			r.ProcessAlertDuration,
			r.PluginDuration,
			r.SupervisorPanic,
			r.HTTPRequestDuration,
			r.DBQueryDuration,
			collectors.NewGoCollector(),
			collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		)
	}
	return r
}
