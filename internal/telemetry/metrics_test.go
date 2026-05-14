package telemetry

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

func TestNewRegistry_ExposesAllMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	r := NewRegistry(reg)
	require.NotNil(t, r)

	r.AlertHit.WithLabelValues("rule", "continue").Inc()
	r.SupervisorPanic.WithLabelValues("pipeline").Inc()
	r.ProcessAlertDuration.WithLabelValues("dispatch").Observe(0.5)
	r.PluginDuration.WithLabelValues("rule", "process").Observe(0.5)
	r.HTTPRequestDuration.WithLabelValues("GET", "/api/v1/rule", "200").Observe(0.5)
	r.DBQueryDuration.WithLabelValues("sqlite", "search", "record").Observe(0.5)

	require.InDelta(t, 1.0, testutil.ToFloat64(r.AlertHit.WithLabelValues("rule", "continue")), 0)
	require.InDelta(t, 1.0, testutil.ToFloat64(r.SupervisorPanic.WithLabelValues("pipeline")), 0)

	mfs, err := reg.Gather()
	require.NoError(t, err)
	names := map[string]bool{}
	for _, mf := range mfs {
		names[mf.GetName()] = true
	}
	want := []string{
		"snooze_alert_hit_total",
		"snooze_process_alert_duration_seconds",
		"snooze_plugin_duration_seconds",
		"snooze_supervisor_panic_total",
		"snooze_http_request_duration_seconds",
		"snooze_db_query_duration_seconds",
	}
	for _, n := range want {
		require.True(t, names[n], "missing metric %s", n)
	}
}

func TestNewRegistry_RegistersDefaultCollectors(t *testing.T) {
	reg := prometheus.NewRegistry()
	NewRegistry(reg)
	mfs, err := reg.Gather()
	require.NoError(t, err)
	var blob strings.Builder
	for _, mf := range mfs {
		blob.WriteString(mf.GetName())
		blob.WriteString("\n")
	}
	out := blob.String()
	require.Contains(t, out, "go_")      // GoCollector
	require.Contains(t, out, "process_") // ProcessCollector
}

func TestNewRegistry_NilRegistererDoesNotRegister(t *testing.T) {
	r := NewRegistry(nil)
	require.NotNil(t, r)
	// Counter is still usable even when not registered.
	r.AlertHit.WithLabelValues("rule", "abort").Inc()
}
