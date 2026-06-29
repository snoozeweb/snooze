package core

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// ProcessRecordMap is the loose-map adapter for the internal/api package's
// AlertProcessor interface. It marshals the incoming map into a typed
// Record, drives the pipeline, and emits the result as a map ready for
// JSON encoding. The pipeline Action is propagated to the caller so that
// handleAlertPost can distinguish a policy-reject ActionAbort from a normal
// successful pass.
//
// This is a thin shim because the API surface predates the typed Record:
// /api/v1/alerts callers post raw JSON, the handler decodes to map[string]any,
// hands the map to AlertProcessor.ProcessRecord, and forwards the result.
//
// Compile-time guarantee that *Core satisfies api.AlertProcessor lives in
// cmd/snooze-server/main.go (wired at boot).
func (c *Core) ProcessRecordMap(ctx context.Context, rec map[string]any) (map[string]any, plugins.Action, error) {
	in, err := mapToRecord(rec)
	if err != nil {
		return nil, plugins.ActionContinue, fmt.Errorf("core: decode incoming record: %w", err)
	}
	out, action, err := c.ProcessRecord(ctx, in)
	if err != nil {
		return nil, plugins.ActionContinue, err
	}
	return recordToMap(out), action, nil
}

// mapToRecord JSON-round-trips the loose map into a typed Record. Unknown
// keys land in the Record.Extra map by way of the unmarshal path's tolerance.
// We deliberately reuse encoding/json rather than hand-coding the field
// mapping so the Record's JSON tags remain the single source of truth.
//
// Custom-source ingest hint: the reserved key `_preserve_raw` is a transient
// sentinel, not a record field. When a posted body carries `_preserve_raw` with
// a truthy value, every unrecognised (extra) key is copied verbatim into
// rec.Raw — archiving the original foreign payload for audit before any rule
// remaps the canonical fields. An explicitly-sent `raw` object is never
// clobbered (existing entries win; foreign keys are merged in). The sentinel is
// consumed here and never reaches the record or the pipeline: it is listed in
// knownRecordKeys (so it is excluded from the Extra projection) and deleted from
// Extra defensively. See docs/content/general/integrations/custom-source.md.
func mapToRecord(m map[string]any) (snoozetypes.Record, error) {
	if m == nil {
		return snoozetypes.Record{}, nil
	}
	buf, err := json.Marshal(m)
	if err != nil {
		return snoozetypes.Record{}, err
	}
	var rec snoozetypes.Record
	if err := json.Unmarshal(buf, &rec); err != nil {
		return snoozetypes.Record{}, err
	}
	// Capture any keys that did not match a typed field into Extra so they
	// survive the round trip through the pipeline.
	known := knownRecordKeys
	for k, v := range m {
		if _, ok := known[k]; ok {
			continue
		}
		if rec.Extra == nil {
			rec.Extra = map[string]any{}
		}
		rec.Extra[k] = v
	}
	// Opt-in raw preservation: copy the captured extra keys into rec.Raw when
	// the caller set `_preserve_raw` truthy. This runs before any rule processor,
	// so a later DELETE modification on a canonical field does not strip the
	// archived original from raw. Existing raw entries are never overwritten.
	if v, ok := m["_preserve_raw"]; ok && isTruthyAny(v) {
		if rec.Raw == nil {
			rec.Raw = map[string]any{}
		}
		for k, val := range rec.Extra {
			if _, exists := rec.Raw[k]; !exists {
				rec.Raw[k] = val
			}
		}
	}
	// The sentinel is never a record field. It is in knownRecordKeys so it is
	// already excluded from the Extra projection above; delete it defensively in
	// case the known-key set is ever edited out of sync.
	delete(rec.Extra, "_preserve_raw")
	if len(rec.Extra) == 0 {
		rec.Extra = nil
	}
	return rec, nil
}

// isTruthyAny reports whether v is a truthy ingest-hint value: a non-zero
// numeric, a non-empty string, or boolean true. It mirrors the loose-truthiness
// a JSON sender expects from a `_preserve_raw` flag (true, 1, "1", "true" all
// enable; false, 0, "" disable).
func isTruthyAny(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return x != "" && x != "false" && x != "0"
	case int:
		return x != 0
	case int32:
		return x != 0
	case int64:
		return x != 0
	case float32:
		return x != 0
	case float64:
		return x != 0
	case nil:
		return false
	}
	return false
}

// recordToMap is the inverse: emits the typed fields with their JSON names,
// then folds Extra back in (typed fields win on key collision).
func recordToMap(rec snoozetypes.Record) map[string]any {
	doc := recordToDoc(rec)
	out := make(map[string]any, len(doc))
	for k, v := range doc {
		out[k] = v
	}
	return out
}

// knownRecordKeys lists the JSON tags of snoozetypes.Record's typed fields,
// plus the reserved `_preserve_raw` ingest sentinel. Listing the sentinel here
// keeps it out of the Extra projection in mapToRecord (it is a transient hint,
// never a record field). Keep the typed entries in sync with
// pkg/snoozetypes/record.go.
var knownRecordKeys = map[string]struct{}{
	"uid":         {},
	"host":        {},
	"source":      {},
	"process":     {},
	"severity":    {},
	"message":     {},
	"timestamp":   {},
	"date_epoch":  {},
	"ttl":         {},
	"environment": {},
	"hash":        {},
	"tags":        {},
	"raw":         {},
	"state":       {},
	"plugins":     {},
	// Reserved ingest hint; consumed by mapToRecord, never stored.
	"_preserve_raw": {},
}
