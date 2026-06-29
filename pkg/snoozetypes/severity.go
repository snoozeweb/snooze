package snoozetypes

import "strings"

// DefaultSeverityRank is Snooze's canonical severity ladder, mirroring the
// frontend web/src/lib/format/severity-color.ts RANK map. Rank 0 is MOST severe;
// higher = less severe. Aliases share a rank. Severities not in the map are
// "unranked" (callers treat absence as not-comparable). Keep this in sync with
// the frontend RANK map — Plan 28 exposes this server copy via /api/v1/config so
// it is authoritative at runtime and the frontend map is only an offline fallback.
//
// It is a var, not a const map, so a future runtime-config path (Plan 28) can
// substitute or extend the ladder at startup.
var DefaultSeverityRank = map[string]int{
	"emerg": 0, "emergency": 0, "panic": 0,
	"alert": 1,
	"crit":  2, "critical": 2, "fatal": 2,
	"err": 3, "error": 3, "fail": 3, "failure": 3,
	"warn": 4, "warning": 4,
	"notice": 5,
	"info":   6, "informational": 6,
	"debug": 7,
	"ok":    8, "okay": 8, "success": 8,
}

// SeverityRank returns (rank, true) if s (case-folded, trimmed) is in the ladder,
// else (0, false).
func SeverityRank(s string) (int, bool) {
	r, ok := DefaultSeverityRank[NormalizeSeverity(s)]
	return r, ok
}

// SeverityVariant folds s into one of six visual buckets mirroring the frontend
// variantOf: "critical" | "error" | "warning" | "info" | "ok" | "muted".
// Unknown severities → "muted". These six names map to the --severity-* theme
// tokens; this function defines the buckets, NOT colours (colours stay theme CSS).
func SeverityVariant(s string) string {
	r, ok := SeverityRank(s)
	switch {
	case !ok:
		return "muted"
	case r <= 2:
		return "critical"
	case r == 3:
		return "error"
	case r == 4:
		return "warning"
	case r <= 7: // 5, 6, 7
		return "info"
	default: // 8
		return "ok"
	}
}

// NormalizeSeverity lowercases + trims (never errors; unknowns pass through).
func NormalizeSeverity(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// CompareSeverity returns -1 / 0 / +1: negative ⇒ a is MORE severe than b
// (lower rank), positive ⇒ less severe. If either severity is unranked, returns
// 0 (no-change) — matching Alerta's trend() "indeterminate" fallback.
func CompareSeverity(a, b string) int {
	ra, oka := SeverityRank(a)
	rb, okb := SeverityRank(b)
	if !oka || !okb {
		return 0
	}
	switch {
	case ra < rb:
		return -1
	case ra > rb:
		return +1
	default:
		return 0
	}
}
