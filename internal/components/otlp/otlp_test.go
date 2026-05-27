package otlp

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestConfig_WithDefaults(t *testing.T) {
	t.Run("requires server", func(t *testing.T) {
		_, err := Config{Listen: ":4318"}.WithDefaults()
		require.Error(t, err)
	})
	t.Run("fills defaults", func(t *testing.T) {
		c, err := Config{Server: "https://snooze.example"}.WithDefaults()
		require.NoError(t, err)
		require.Equal(t, ":4318", c.Listen)
		require.Equal(t, "local", c.Method)
		require.Equal(t, 30*time.Second, c.RequestTimeout)
	})
}

func TestLoadConfig_FromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "otlp.yaml")
	body := []byte(`
server: https://snooze.example/
username: ingest
password: hunter2
listen: 127.0.0.1:4318
request_timeout: 5s
debug: true
`)
	require.NoError(t, writeFile(path, body))
	c, err := LoadConfig(path)
	require.NoError(t, err)
	require.Equal(t, "https://snooze.example/", c.Server)
	require.Equal(t, "ingest", c.Username)
	require.Equal(t, "127.0.0.1:4318", c.Listen)
	require.Equal(t, 5*time.Second, c.RequestTimeout)
	require.True(t, c.Debug)
}

func TestSeverityFromNumber(t *testing.T) {
	cases := []struct {
		n    int
		want string
	}{
		{0, ""},                    // unspecified
		{1, "debug"}, {4, "debug"}, // TRACE
		{5, "debug"}, {8, "debug"}, // DEBUG
		{9, "info"}, {12, "info"}, // INFO
		{13, "warning"}, {16, "warning"}, // WARN
		{17, "error"}, {20, "error"}, // ERROR
		{21, "critical"}, {24, "critical"}, // FATAL
		{99, ""}, // out of range
	}
	for _, c := range cases {
		require.Equalf(t, c.want, severityFromNumber(c.n), "severityNumber=%d", c.n)
	}
}

func TestNormalizeSeverityText(t *testing.T) {
	require.Equal(t, "", normalizeSeverityText(""))
	require.Equal(t, "debug", normalizeSeverityText("TRACE"))
	require.Equal(t, "debug", normalizeSeverityText("Debug"))
	require.Equal(t, "info", normalizeSeverityText("INFO"))
	require.Equal(t, "info", normalizeSeverityText("notice"))
	require.Equal(t, "warning", normalizeSeverityText("Warn"))
	require.Equal(t, "error", normalizeSeverityText("ERROR"))
	require.Equal(t, "critical", normalizeSeverityText("FATAL"))
	require.Equal(t, "critical", normalizeSeverityText("emergency"))
	// Unknown custom level → lower-cased passthrough.
	require.Equal(t, "custom", normalizeSeverityText("Custom"))
}

func TestResolveSeverity_NumberWinsThenTextThenDefault(t *testing.T) {
	// Number present → used even when text disagrees.
	require.Equal(t, "error", resolveSeverity(logRecord{SeverityNumber: 17, SeverityText: "INFO"}))
	// No number → text used.
	require.Equal(t, "warning", resolveSeverity(logRecord{SeverityText: "Warning"}))
	// Neither → default info.
	require.Equal(t, "info", resolveSeverity(logRecord{}))
}

func TestParseUnixNano(t *testing.T) {
	// proto3-JSON encodes uint64 as a quoted string.
	got := parseUnixNano(json.RawMessage(`"1700000000000000000"`))
	require.False(t, got.IsZero())
	require.Equal(t, int64(1700000000), got.Unix())
	// Bare-number tolerance.
	got = parseUnixNano(json.RawMessage(`1700000000000000000`))
	require.Equal(t, int64(1700000000), got.Unix())
	// Empty / zero → zero time.
	require.True(t, parseUnixNano(nil).IsZero())
	require.True(t, parseUnixNano(json.RawMessage(`"0"`)).IsZero())
	require.True(t, parseUnixNano(json.RawMessage(`""`)).IsZero())
}

// sampleLogsJSON is a realistic OTLP-JSON ExportLogsServiceRequest carrying one
// WARN log record with resource + log attributes.
const sampleLogsJSON = `{
  "resourceLogs": [{
    "resource": {
      "attributes": [
        {"key": "host.name", "value": {"stringValue": "web-01"}},
        {"key": "service.name", "value": {"stringValue": "checkout"}},
        {"key": "service.instance.id", "value": {"stringValue": "checkout-7f9"}}
      ]
    },
    "scopeLogs": [{
      "logRecords": [{
        "timeUnixNano": "1700000000000000000",
        "severityNumber": 13,
        "severityText": "WARN",
        "body": {"stringValue": "disk usage at 92%"},
        "attributes": [
          {"key": "disk.path", "value": {"stringValue": "/var"}},
          {"key": "disk.percent", "value": {"intValue": "92"}},
          {"key": "alerting", "value": {"boolValue": true}}
        ]
      }]
    }]
  }]
}`

func TestRecordsFromRequest_FieldMapping(t *testing.T) {
	var req exportLogsServiceRequest
	require.NoError(t, json.Unmarshal([]byte(sampleLogsJSON), &req))

	now := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	recs := recordsFromRequest(req, now)
	require.Len(t, recs, 1)
	rec := recs[0]

	require.Equal(t, "otlp", rec.Source)
	require.Equal(t, "web-01", rec.Host)      // host.name wins
	require.Equal(t, "checkout", rec.Process) // service.name
	require.Equal(t, "warning", rec.Severity) // severityNumber 13 → warning
	require.Equal(t, "disk usage at 92%", rec.Message)
	require.Equal(t, int64(1700000000), rec.Timestamp.Unix())

	// Raw merges resource + log attributes plus the raw severity inputs.
	require.Equal(t, "web-01", rec.Raw["host.name"])
	require.Equal(t, "checkout", rec.Raw["service.name"])
	require.Equal(t, "/var", rec.Raw["disk.path"])
	require.Equal(t, int64(92), rec.Raw["disk.percent"]) // intValue decoded to int64
	require.Equal(t, true, rec.Raw["alerting"])
	require.Equal(t, 13, rec.Raw["severity_number"])
	require.Equal(t, "WARN", rec.Raw["severity_text"])
}

func TestRecordsFromRequest_HostFallbackAndTimestampDefault(t *testing.T) {
	const noHostName = `{
      "resourceLogs": [{
        "resource": {"attributes": [
          {"key": "service.instance.id", "value": {"stringValue": "inst-9"}}
        ]},
        "scopeLogs": [{"logRecords": [
          {"body": {"stringValue": "hello"}}
        ]}]
      }]
    }`
	var req exportLogsServiceRequest
	require.NoError(t, json.Unmarshal([]byte(noHostName), &req))

	now := time.Date(2021, 6, 1, 12, 0, 0, 0, time.UTC)
	recs := recordsFromRequest(req, now)
	require.Len(t, recs, 1)
	require.Equal(t, "inst-9", recs[0].Host)   // falls back to service.instance.id
	require.Equal(t, "info", recs[0].Severity) // no number/text → default
	require.Equal(t, now, recs[0].Timestamp)   // no timeUnixNano → caller's now
}

func TestRecordsFromRequest_MultipleRecords(t *testing.T) {
	const twoLogs = `{
      "resourceLogs": [{
        "resource": {"attributes": [{"key": "host.name", "value": {"stringValue": "h1"}}]},
        "scopeLogs": [{"logRecords": [
          {"severityNumber": 21, "body": {"stringValue": "boom"}},
          {"severityNumber": 9, "body": {"stringValue": "fyi"}}
        ]}]
      }]
    }`
	var req exportLogsServiceRequest
	require.NoError(t, json.Unmarshal([]byte(twoLogs), &req))
	recs := recordsFromRequest(req, time.Now())
	require.Len(t, recs, 2)
	require.Equal(t, "critical", recs[0].Severity)
	require.Equal(t, "boom", recs[0].Message)
	require.Equal(t, "info", recs[1].Severity)
}

func TestBodyAsString_NonStringValues(t *testing.T) {
	require.Equal(t, "42", anyValue{IntValue: json.RawMessage(`"42"`)}.asString())
	require.Equal(t, "true", anyValue{BoolValue: boolPtr(true)}.asString())
	require.Equal(t, "", anyValue{}.asString())
}

func boolPtr(b bool) *bool { return &b }
