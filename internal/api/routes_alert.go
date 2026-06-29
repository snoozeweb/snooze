package api

import (
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/snoozeweb/snooze/internal/api/middleware"
	"github.com/snoozeweb/snooze/internal/plugins"
)

// mountAlerts wires POST /api/v1/alerts.
//
// The handler accepts a single record or an array, calls Processor.ProcessRecord
// on each, then writes a small batch envelope: {data: [...], errors: [...]}.
func (rt *Router) mountAlerts(r chi.Router) {
	if rt.Processor == nil {
		return
	}
	resolver := rt.TenantResolver
	if resolver == nil {
		resolver = middleware.NewTenantResolver()
	}
	r.Route("/api/v1/alerts", func(sub chi.Router) {
		sub.Use(middleware.IngestTenant(resolver, rt.TenantChecker))
		sub.Post("/", rt.handleAlertPost)
	})
}

// handleAlertPost ingests one or many alert payloads.
//
// Per-record results:
//   - ActionContinue / ActionAbortWrite / ActionAbortUpdate with no error:
//     record appended to data (or omitted for nil result).
//   - ActionAbort with reject_policy set: policy rejection accumulated.
//   - ActionAbort without reject_policy (snooze discard): silent drop, 200.
//   - Error returned: appended to errors[].
//
// Response codes:
//   - 200 when all records succeed or are silently discarded.
//   - 422 when every record in the batch was rejected by a reject policy.
//   - 200 for a mixed batch (some pass, some policy-rejected): successful
//     records land in data[], rejection reasons land in errors[].
func (rt *Router) handleAlertPost(w http.ResponseWriter, r *http.Request) {
	if rt.Processor == nil {
		WriteError(w, r, ErrUnavailable.WithMessage("alert ingestion disabled"))
		return
	}
	records, err := ParseJSONOrArray(r)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	// Stamp the resolved HTTP client IP onto every record as provenance,
	// unless the caller supplied a non-empty source_ip of their own.
	ip := middleware.ClientIP(r)
	out := struct {
		Data   []map[string]any `json:"data"`
		Errors []string         `json:"errors,omitempty"`
	}{
		Data: make([]map[string]any, 0, len(records)),
	}

	rejections := 0
	for _, rec := range records {
		if v, _ := rec["source_ip"].(string); v == "" {
			rec["source_ip"] = ip
		}
		res, action, err := rt.Processor.ProcessRecord(r.Context(), rec)
		if err != nil {
			out.Errors = append(out.Errors, err.Error())
			continue
		}
		// Detect a reject-policy abort: ActionAbort + "reject_policy" field set.
		if action == plugins.ActionAbort {
			if ruleName, ok := res["reject_policy"].(string); ok && ruleName != "" {
				out.Errors = append(out.Errors, fmt.Sprintf("policy_rejected: %s", ruleName))
				rejections++
				continue
			}
			// Plain discard (snooze plugin with discard=true): drop silently, 200.
			continue
		}
		if res != nil {
			out.Data = append(out.Data, res)
		}
	}

	// If every record in the batch was policy-rejected, return 422.
	if rejections > 0 && rejections == len(records) {
		WriteError(w, r, ErrPolicyRejected.WithMessage(out.Errors[0]))
		return
	}
	WriteJSON(w, http.StatusOK, out)
}
