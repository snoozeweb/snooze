// Package receiverutil holds small, pure state-mapping helpers shared across
// webhook-receiver plugins (newrelic, and later stackdriver/pagerduty).
//
// It is deliberately not a plugin: there is no init, no metadata.yaml, and the
// only dependency is the standard library. Keeping the mapping here lets sibling
// receiver packages reuse the Alerta-style state convention without importing
// one another.
package receiverutil

import "strings"

// MapLegacyState maps an upstream string to a Snooze record state, following the
// Alerta convention:
//
//	"acknowledged" (case-insensitive)       → "ack"
//	"closed" / "resolved" (case-insensitive) → "close"
//	anything else (incl. "", "open", "pending") → "" (firing, no explicit state)
func MapLegacyState(raw string) string {
	switch strings.ToLower(raw) {
	case "acknowledged":
		return "ack"
	case "closed", "resolved":
		return "close"
	default:
		return ""
	}
}
