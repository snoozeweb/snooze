package otlp

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// The types below mirror the OTLP/HTTP JSON encoding of the logs signal
// (ExportLogsServiceRequest). They are a deliberately partial transcription of
// the OpenTelemetry proto3-JSON mapping: only the fields the receiver maps onto
// a snoozetypes.Record are modelled. Unknown fields are tolerated by
// encoding/json. See:
// https://opentelemetry.io/docs/specs/otlp/ and the proto3-JSON mapping.

// exportLogsServiceRequest is the top-level JSON body POSTed to /v1/logs.
type exportLogsServiceRequest struct {
	ResourceLogs []resourceLogs `json:"resourceLogs"`
}

// resourceLogs groups scopeLogs sharing one resource (e.g. one service/host).
type resourceLogs struct {
	Resource  resource    `json:"resource"`
	ScopeLogs []scopeLogs `json:"scopeLogs"`
}

// resource carries the attributes describing the producer of the logs.
type resource struct {
	Attributes []keyValue `json:"attributes"`
}

// scopeLogs groups logRecords emitted by one instrumentation scope.
type scopeLogs struct {
	LogRecords []logRecord `json:"logRecords"`
}

// logRecord is a single OTLP log entry — the unit mapped to one Record.
type logRecord struct {
	// TimeUnixNano is a uint64 nanoseconds-since-epoch, encoded as a JSON
	// string per the proto3-JSON int64/uint64 rule (but we tolerate a number
	// too via json.Number-style decoding in parseUnixNano).
	TimeUnixNano   json.RawMessage `json:"timeUnixNano"`
	SeverityNumber int             `json:"severityNumber"`
	SeverityText   string          `json:"severityText"`
	Body           anyValue        `json:"body"`
	Attributes     []keyValue      `json:"attributes"`
}

// keyValue is one attribute: a string key paired with a typed value.
type keyValue struct {
	Key   string   `json:"key"`
	Value anyValue `json:"value"`
}

// anyValue is the OTLP AnyValue: a oneof over the supported scalar/compound
// value types. We decode the handful that carry useful alert content and fall
// back to a JSON rendering for the rest.
type anyValue struct {
	StringValue *string         `json:"stringValue,omitempty"`
	BoolValue   *bool           `json:"boolValue,omitempty"`
	IntValue    json.RawMessage `json:"intValue,omitempty"` // int64 → JSON string per proto3-JSON
	DoubleValue *float64        `json:"doubleValue,omitempty"`
	ArrayValue  json.RawMessage `json:"arrayValue,omitempty"`
	KvlistValue json.RawMessage `json:"kvlistValue,omitempty"`
	BytesValue  *string         `json:"bytesValue,omitempty"`
}

// asGo converts the AnyValue into a plain Go value suitable for Record.Raw.
// Returns (nil, false) when the value is empty/unset.
func (v anyValue) asGo() (any, bool) {
	switch {
	case v.StringValue != nil:
		return *v.StringValue, true
	case v.BoolValue != nil:
		return *v.BoolValue, true
	case len(v.IntValue) > 0:
		if n, ok := jsonIntToInt64(v.IntValue); ok {
			return n, true
		}
		return strings.Trim(string(v.IntValue), `"`), true
	case v.DoubleValue != nil:
		return *v.DoubleValue, true
	case v.BytesValue != nil:
		return *v.BytesValue, true
	case len(v.ArrayValue) > 0:
		return decodeRawJSON(v.ArrayValue), true
	case len(v.KvlistValue) > 0:
		return decodeRawJSON(v.KvlistValue), true
	default:
		return nil, false
	}
}

// asString renders the AnyValue as a plain string for use in scalar fields
// (Message, Host, Process). Compound values are rendered as compact JSON.
func (v anyValue) asString() string {
	g, ok := v.asGo()
	if !ok {
		return ""
	}
	switch t := g.(type) {
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		return strconv.FormatFloat(t, 'g', -1, 64)
	default:
		raw, err := json.Marshal(t)
		if err != nil {
			return fmt.Sprintf("%v", t)
		}
		return string(raw)
	}
}

// decodeRawJSON parses a json.RawMessage into a generic any (used for
// array/kvlist attribute values that we pass through to Raw verbatim).
func decodeRawJSON(raw json.RawMessage) any {
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		return string(raw)
	}
	return out
}

// jsonIntToInt64 parses an OTLP int64 — which proto3-JSON encodes as a quoted
// string, though some emitters send a bare number. Both are accepted.
func jsonIntToInt64(raw json.RawMessage) (int64, bool) {
	s := strings.TrimSpace(string(raw))
	s = strings.Trim(s, `"`)
	if s == "" {
		return 0, false
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

// parseUnixNano decodes timeUnixNano (a uint64, proto3-JSON-encoded as a quoted
// string, occasionally as a bare number) into a time.Time. A zero/absent value
// yields the zero Time so the caller can substitute time.Now.
func parseUnixNano(raw json.RawMessage) time.Time {
	if len(raw) == 0 {
		return time.Time{}
	}
	s := strings.Trim(strings.TrimSpace(string(raw)), `"`)
	if s == "" || s == "0" {
		return time.Time{}
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n <= 0 {
		return time.Time{}
	}
	return time.Unix(0, n).UTC()
}

// severityFromNumber maps an OTLP SeverityNumber onto a Snooze severity
// keyword, following the bucketing in the OTLP logs data model:
//
//	1-4   TRACE → debug
//	5-8   DEBUG → debug
//	9-12  INFO  → info
//	13-16 WARN  → warning
//	17-20 ERROR → error
//	21-24 FATAL → critical
//
// Out-of-range / unspecified (0) returns "" so the caller can fall back to
// severityText or the default.
func severityFromNumber(n int) string {
	switch {
	case n >= 1 && n <= 8:
		return "debug"
	case n >= 9 && n <= 12:
		return "info"
	case n >= 13 && n <= 16:
		return "warning"
	case n >= 17 && n <= 20:
		return "error"
	case n >= 21 && n <= 24:
		return "critical"
	default:
		return ""
	}
}

// normalizeSeverityText maps a free-form OTLP severityText onto a canonical
// Snooze severity keyword. Unknown text is lower-cased and returned as-is so
// custom levels still propagate; empty input returns "".
func normalizeSeverityText(s string) string {
	t := strings.ToLower(strings.TrimSpace(s))
	switch t {
	case "":
		return ""
	case "trace", "trace2", "trace3", "trace4":
		return "debug"
	case "debug", "debug2", "debug3", "debug4":
		return "debug"
	case "info", "information", "informational", "info2", "info3", "info4", "notice":
		return "info"
	case "warn", "warning", "warn2", "warn3", "warn4":
		return "warning"
	case "error", "err", "error2", "error3", "error4":
		return "error"
	case "fatal", "fatal2", "fatal3", "fatal4", "critical", "crit", "emergency", "alert", "panic":
		return "critical"
	default:
		return t
	}
}

// resolveSeverity picks the final Snooze severity for a record. The numeric
// SeverityNumber wins when present (it is the OTLP-canonical signal); otherwise
// the textual SeverityText is normalised; failing both, "info" is the default.
func resolveSeverity(rec logRecord) string {
	if sev := severityFromNumber(rec.SeverityNumber); sev != "" {
		return sev
	}
	if sev := normalizeSeverityText(rec.SeverityText); sev != "" {
		return sev
	}
	return "info"
}

// attrMap flattens a slice of OTLP attributes into a string→any map, decoding
// each AnyValue to a plain Go value. Empty values are skipped.
func attrMap(attrs []keyValue) map[string]any {
	out := make(map[string]any, len(attrs))
	for _, a := range attrs {
		if a.Key == "" {
			continue
		}
		if v, ok := a.Value.asGo(); ok {
			out[a.Key] = v
		}
	}
	return out
}

// attrString returns the first non-empty attribute value (as a string) for any
// of keys, scanning in order. Used for the host/service fallback chains.
func attrString(attrs []keyValue, keys ...string) string {
	for _, want := range keys {
		for _, a := range attrs {
			if a.Key == want {
				if s := a.Value.asString(); strings.TrimSpace(s) != "" {
					return s
				}
			}
		}
	}
	return ""
}

// recordsFromRequest walks an ExportLogsServiceRequest and maps every
// logRecord onto a snoozetypes.Record. Resource attributes are applied to every
// record beneath the resource; per-log attributes are merged on top (log
// attributes win on a key clash). The mapping is:
//
//   - Source:    "otlp"
//   - Severity:  severityNumber bucket, else severityText, else "info"
//   - Host:      resource "host.name", falling back to "service.instance.id"
//   - Process:   resource "service.name"
//   - Message:   logRecord body.stringValue (or rendered AnyValue)
//   - Timestamp: timeUnixNano (else time.Now at the call site)
//   - Raw:       merged resource + log attributes, plus the raw severity inputs
func recordsFromRequest(req exportLogsServiceRequest, now time.Time) []snoozetypes.Record {
	var out []snoozetypes.Record
	for _, rl := range req.ResourceLogs {
		resAttrs := rl.Resource.Attributes
		host := attrString(resAttrs, "host.name", "service.instance.id")
		process := attrString(resAttrs, "service.name")
		resMap := attrMap(resAttrs)

		for _, sl := range rl.ScopeLogs {
			for _, lr := range sl.LogRecords {
				ts := parseUnixNano(lr.TimeUnixNano)
				if ts.IsZero() {
					ts = now
				}

				// Merge resource + log attributes into Raw (log wins).
				raw := make(map[string]any, len(resMap)+len(lr.Attributes)+2)
				for k, v := range resMap {
					raw[k] = v
				}
				for k, v := range attrMap(lr.Attributes) {
					raw[k] = v
				}
				if lr.SeverityNumber != 0 {
					raw["severity_number"] = lr.SeverityNumber
				}
				if lr.SeverityText != "" {
					raw["severity_text"] = lr.SeverityText
				}

				rec := snoozetypes.Record{
					Source:    "otlp",
					Host:      host,
					Process:   process,
					Severity:  resolveSeverity(lr),
					Message:   lr.Body.asString(),
					Timestamp: ts,
					Raw:       raw,
				}
				out = append(out, rec)
			}
		}
	}
	return out
}
