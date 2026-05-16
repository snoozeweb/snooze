package notification

import (
	"encoding/json"
	"time"

	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// decodeEntry converts a free-form db.Document into a typed Entry by round-
// tripping through JSON. The cost is acceptable here because Reload happens
// out of the hot path (sync/CRUD events only).
func decodeEntry(d db.Document) (Entry, bool) {
	if d == nil {
		return Entry{}, false
	}
	raw, err := json.Marshal(d)
	if err != nil {
		return Entry{}, false
	}
	var e Entry
	if err := json.Unmarshal(raw, &e); err != nil {
		return Entry{}, false
	}
	if e.Name == "" {
		return Entry{}, false
	}
	return e, true
}

// recordToMap projects the typed snoozetypes.Record into the loose map shape
// expected by the condition evaluator. Empty/zero fields are elided so the
// evaluator's EXISTS / NULL semantics match the Python implementation.
func recordToMap(rec snoozetypes.Record) map[string]any {
	m := make(map[string]any, 16)
	if rec.UID != "" {
		m["uid"] = rec.UID
	}
	if rec.Host != "" {
		m["host"] = rec.Host
	}
	if rec.Source != "" {
		m["source"] = rec.Source
	}
	if rec.Process != "" {
		m["process"] = rec.Process
	}
	if rec.Severity != "" {
		m["severity"] = rec.Severity
	}
	if rec.Message != "" {
		m["message"] = rec.Message
	}
	if !rec.Timestamp.IsZero() {
		m["timestamp"] = rec.Timestamp
	}
	if rec.DateEpoch != 0 {
		m["date_epoch"] = rec.DateEpoch
	}
	if rec.TTL != 0 {
		m["ttl"] = rec.TTL
	}
	if rec.Environment != "" {
		m["environment"] = rec.Environment
	}
	if len(rec.Tags) > 0 {
		m["tags"] = rec.Tags
	}
	if len(rec.Raw) > 0 {
		m["raw"] = rec.Raw
	}
	if rec.State != "" {
		m["state"] = rec.State
	}
	if len(rec.Plugins) > 0 {
		m["plugins"] = rec.Plugins
	}
	for k, v := range rec.Extra {
		if _, exists := m[k]; !exists {
			m[k] = v
		}
	}
	return m
}

// recordTime picks the timestamp the time-constraint evaluator should use.
// Mirrors Python's `get_record_date`: prefer the record's `timestamp`, fall
// back to `date_epoch`, finally `time.Now()`.
func recordTime(rec snoozetypes.Record) time.Time {
	if !rec.Timestamp.IsZero() {
		return rec.Timestamp
	}
	if rec.DateEpoch != 0 {
		return time.Unix(rec.DateEpoch, 0)
	}
	return time.Now()
}
