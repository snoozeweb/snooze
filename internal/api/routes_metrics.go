package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// mountMetrics wires the /metrics endpoint backed by promhttp.HandlerFor on
// the provided Gatherer. When no gatherer is configured we fall back to the
// process-wide default registry.
func (rt *Router) mountMetrics(r chi.Router) {
	var gatherer = prometheus.DefaultGatherer
	if rt.MetricsGatherer != nil {
		gatherer = rt.MetricsGatherer
	}
	handler := promhttp.HandlerFor(gatherer, promhttp.HandlerOpts{
		ErrorHandling: promhttp.ContinueOnError,
		Registry:      nil,
	})
	r.Method(http.MethodGet, "/metrics", handler)
}
