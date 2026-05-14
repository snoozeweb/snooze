package core

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/japannext/snooze/pkg/snoozetypes"
)

// ProcessRecordMap is the loose-map adapter for the internal/api package's
// AlertProcessor interface. It marshals the incoming map into a typed
// Record, drives the pipeline, and emits the result as a map ready for
// JSON encoding.
//
// This is a thin shim because the API surface predates the typed Record:
// /api/v1/alerts callers post raw JSON, the handler decodes to map[string]any,
// hands the map to AlertProcessor.ProcessRecord, and forwards the result.
//
// Compile-time guarantee that *Core satisfies api.AlertProcessor lives in
// cmd/snooze-server/main.go (wired at boot).
func (c *Core) ProcessRecordMap(ctx context.Context, rec map[string]any) (map[string]any, error) {
	in, err := mapToRecord(rec)
	if err != nil {
		return nil, fmt.Errorf("core: decode incoming record: %w", err)
	}
	out, _, err := c.ProcessRecord(ctx, in)
	if err != nil {
		return nil, err
	}
	return recordToMap(out), nil
}

// mapToRecord JSON-round-trips the loose map into a typed Record. Unknown
// keys land in the Record.Extra map by way of the unmarshal path's tolerance.
// We deliberately reuse encoding/json rather than hand-coding the field
// mapping so the Record's JSON tags remain the single source of truth.
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
	return rec, nil
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

// knownRecordKeys lists the JSON tags of snoozetypes.Record's typed fields.
// Keep in sync with pkg/snoozetypes/record.go.
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
	"tags":        {},
	"raw":         {},
	"state":       {},
	"plugins":     {},
}
