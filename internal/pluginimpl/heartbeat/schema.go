package heartbeat

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// heartbeat is the typed view of a heartbeat document. It is parsed loosely
// from the free-form db.Document map because JSON numbers arrive as float64
// (or json.Number) and last_seen may be RFC3339 or an epoch.
type heartbeat struct {
	Name        string
	Token       string // server-generated ping secret; required by HandleWebhook
	Interval    int64  // seconds
	Grace       int64  // seconds (optional, default 0)
	Severity    string
	Environment string
	Host        string // optional explicit host; falls back to Name
	Message     string // optional custom message
	Enabled     bool

	// LastSeen is the parsed last-ping time. Zero when never pinged.
	LastSeen time.Time
	// LastSeenRaw is the original last_seen value as stored, used both for the
	// fired-set key and the alert label.
	LastSeenRaw string
}

// window returns the silence budget (interval+grace) as a duration.
func (hb heartbeat) window() time.Duration {
	secs := hb.Interval + hb.Grace
	if secs < 0 {
		secs = 0
	}
	return time.Duration(secs) * time.Second
}

// parseHeartbeat builds a heartbeat from a stored document. It returns ok=false
// when the document lacks a usable name or interval — such rows are skipped by
// the scanner rather than crashing it.
func parseHeartbeat(doc map[string]any) (heartbeat, bool) {
	hb := heartbeat{Enabled: true, Severity: defaultSeverity}

	name, _ := stringField(doc, "name")
	if name == "" {
		return heartbeat{}, false
	}
	hb.Name = name

	if tok, ok := stringField(doc, "token"); ok {
		hb.Token = tok
	}

	interval, ok := intField(doc, "interval")
	if !ok || interval <= 0 {
		return heartbeat{}, false
	}
	hb.Interval = interval

	if grace, ok := intField(doc, "grace"); ok && grace > 0 {
		hb.Grace = grace
	}
	if sev, ok := stringField(doc, "severity"); ok && sev != "" {
		hb.Severity = sev
	}
	if env, ok := stringField(doc, "environment"); ok {
		hb.Environment = env
	}
	if host, ok := stringField(doc, "host"); ok {
		hb.Host = host
	}
	if msg, ok := stringField(doc, "message"); ok {
		hb.Message = msg
	}
	if en, ok := boolField(doc, "enabled"); ok {
		hb.Enabled = en
	}

	if raw, ok := doc["last_seen"]; ok {
		hb.LastSeenRaw, hb.LastSeen = parseLastSeen(raw)
	}

	return hb, true
}

// parseLastSeen accepts an RFC3339 string or an epoch (seconds, as a number or
// numeric string) and returns the canonical string label plus the parsed time.
// An unparseable value yields the zero time (treated as "never seen").
func parseLastSeen(v any) (string, time.Time) {
	switch t := v.(type) {
	case string:
		if t == "" {
			return "", time.Time{}
		}
		if parsed, err := time.Parse(time.RFC3339, t); err == nil {
			return t, parsed.UTC()
		}
		// Maybe a numeric string epoch.
		if n, err := strconv.ParseInt(t, 10, 64); err == nil {
			return t, time.Unix(n, 0).UTC()
		}
		return t, time.Time{}
	case json.Number:
		if n, err := t.Int64(); err == nil {
			return t.String(), time.Unix(n, 0).UTC()
		}
		return t.String(), time.Time{}
	case float64:
		n := int64(t)
		return strconv.FormatInt(n, 10), time.Unix(n, 0).UTC()
	case int64:
		return strconv.FormatInt(t, 10), time.Unix(t, 0).UTC()
	case int:
		return strconv.Itoa(t), time.Unix(int64(t), 0).UTC()
	}
	return "", time.Time{}
}

// stringField returns the string form of doc[k] and whether it was present and
// a string.
func stringField(doc map[string]any, k string) (string, bool) {
	v, ok := doc[k]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	if !ok {
		return "", false
	}
	return s, true
}

// intField extracts an integer from doc[k], tolerating float64, json.Number,
// int, and numeric strings (JSON decoders use float64 by default).
func intField(doc map[string]any, k string) (int64, bool) {
	v, ok := doc[k]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return int64(n), true
	case int64:
		return n, true
	case int:
		return int64(n), true
	case json.Number:
		i, err := n.Int64()
		return i, err == nil
	case string:
		i, err := strconv.ParseInt(strings.TrimSpace(n), 10, 64)
		return i, err == nil
	}
	return 0, false
}

// boolField extracts a bool from doc[k], tolerating "true"/"false" strings.
func boolField(doc map[string]any, k string) (bool, bool) {
	v, ok := doc[k]
	if !ok {
		return false, false
	}
	switch b := v.(type) {
	case bool:
		return b, true
	case string:
		parsed, err := strconv.ParseBool(b)
		return parsed, err == nil
	}
	return false, false
}

// Schema returns the JSON Schema descriptor for heartbeat documents. The
// frontend and API consumers use it to render and validate the CRUD form.
func (p *Plugin) Schema() any {
	return map[string]any{
		"type":     "object",
		"required": []string{"name", "interval"},
		"properties": map[string]any{
			"name": map[string]any{
				"type":        "string",
				"minLength":   1,
				"description": "Unique heartbeat name; pinged via ?name=<name>&token=<token>.",
			},
			"token": map[string]any{
				"type":        "string",
				"readOnly":    true,
				"description": "Server-generated ping secret. Supply as ?token=<token> on every ping. Never required from the operator on create; generated automatically.",
			},
			"interval": map[string]any{
				"type":        "integer",
				"minimum":     1,
				"description": "Expected ping period in seconds.",
			},
			"grace": map[string]any{
				"type":        "integer",
				"minimum":     0,
				"description": "Extra slack in seconds before a miss fires (optional).",
			},
			"last_seen": map[string]any{
				"type":        "string",
				"description": "Last ping time (RFC3339 or epoch seconds); set by the ping endpoint.",
			},
			"environment": map[string]any{
				"type": "string",
			},
			"host": map[string]any{
				"type":        "string",
				"description": "Alert host; defaults to the heartbeat name.",
			},
			"severity": map[string]any{
				"type":        "string",
				"default":     defaultSeverity,
				"description": "Severity of the miss alert.",
			},
			"enabled": map[string]any{
				"type":        "boolean",
				"default":     true,
				"description": "Disabled heartbeats are not scanned.",
			},
			"message": map[string]any{
				"type":        "string",
				"description": "Optional custom miss-alert message.",
			},
		},
		"additionalProperties": true,
	}
}

// Validate runs structural validation against a decoded heartbeat document
// before it is persisted. It is invoked by the generic CRUD handlers on
// POST/PUT/PATCH bodies.
//
// PATCH bodies are partial: a field that is absent is not validated. When a
// field is present, its type and range are checked. The `name` and `interval`
// requirement is only enforced on full documents (where neither is a partial
// patch) — for partial patches we validate only the fields supplied.
func (p *Plugin) Validate(obj map[string]any) error {
	full := isFullDocument(obj)

	if full {
		name, ok := stringField(obj, "name")
		if !ok || strings.TrimSpace(name) == "" {
			return fmt.Errorf("heartbeat: 'name' is required and must be a non-empty string")
		}
	} else if raw, present := obj["name"]; present {
		if s, ok := raw.(string); !ok || strings.TrimSpace(s) == "" {
			return fmt.Errorf("heartbeat: 'name' must be a non-empty string")
		}
	}

	if raw, present := obj["interval"]; present {
		n, ok := intField(obj, "interval")
		if !ok {
			return fmt.Errorf("heartbeat: 'interval' must be an integer number of seconds (got %T)", raw)
		}
		if n <= 0 {
			return fmt.Errorf("heartbeat: 'interval' must be a positive number of seconds")
		}
	} else if full {
		return fmt.Errorf("heartbeat: 'interval' is required")
	}

	if raw, present := obj["grace"]; present {
		n, ok := intField(obj, "grace")
		if !ok {
			return fmt.Errorf("heartbeat: 'grace' must be an integer number of seconds (got %T)", raw)
		}
		if n < 0 {
			return fmt.Errorf("heartbeat: 'grace' must not be negative")
		}
	}

	if raw, present := obj["enabled"]; present {
		if _, ok := boolField(obj, "enabled"); !ok {
			return fmt.Errorf("heartbeat: 'enabled' must be a boolean (got %T)", raw)
		}
	}

	for _, k := range []string{"severity", "environment", "host", "message", "token"} {
		if raw, present := obj[k]; present {
			if _, ok := raw.(string); !ok {
				return fmt.Errorf("heartbeat: %q must be a string (got %T)", k, raw)
			}
		}
	}

	return nil
}

// isFullDocument heuristically reports whether obj is a complete create/replace
// body (vs a partial PATCH). CRUD PATCH bodies are keyed by {uid} in the URL
// and carry only the fields being changed; they do not re-send the immutable
// `name`. So a body that carries `name` is treated as a full create/replace
// (and must therefore also carry a valid `interval`), while a body without
// `name` is treated as a partial patch and only its present fields are checked.
func isFullDocument(obj map[string]any) bool {
	_, hasName := obj["name"]
	return hasName
}
