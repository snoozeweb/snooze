package snoozeclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// alertsPath is the v1 endpoint for alert ingestion.
const alertsPath = "/api/v1/alerts"

// alertResponse mirrors the wire envelope returned by POST /api/v1/alerts.
//
// The server wraps results in {"data": [...], "errors": [...]} so callers can
// post one record or a batch through the same endpoint.
type alertResponse struct {
	Data   []map[string]any `json:"data"`
	Errors []string         `json:"errors,omitempty"`
}

// PostAlert posts a single Record to /api/v1/alerts and returns the server's
// view of the processed record. The endpoint accepts batches, but this
// helper sends one record and expects either:
//
//   - data[0] populated → returned as snoozetypes.Record.
//   - errors[0] populated → surfaced as a plain error.
//   - both empty → an empty Record is returned (legitimate when the alert
//     was dropped by a route plugin).
func (c *Client) PostAlert(ctx context.Context, rec snoozetypes.Record) (snoozetypes.Record, error) {
	var resp alertResponse
	if err := c.Post(ctx, alertsPath, rec, &resp); err != nil {
		return snoozetypes.Record{}, err
	}
	if len(resp.Errors) > 0 {
		return snoozetypes.Record{}, fmt.Errorf("snoozeclient: alert rejected: %s", resp.Errors[0])
	}
	if len(resp.Data) == 0 {
		return snoozetypes.Record{}, nil
	}
	return recordFromMap(resp.Data[0])
}

// PostAlerts posts a batch of Records and returns the server-side view of each.
// Per-record errors are surfaced as the second return value so callers can
// correlate successes and failures positionally.
func (c *Client) PostAlerts(ctx context.Context, recs []snoozetypes.Record) ([]snoozetypes.Record, []error, error) {
	if len(recs) == 0 {
		return nil, nil, errors.New("snoozeclient: empty batch")
	}
	var resp alertResponse
	if err := c.Post(ctx, alertsPath, recs, &resp); err != nil {
		return nil, nil, err
	}
	out := make([]snoozetypes.Record, 0, len(resp.Data))
	for _, m := range resp.Data {
		rec, err := recordFromMap(m)
		if err != nil {
			return nil, nil, err
		}
		out = append(out, rec)
	}
	errs := make([]error, 0, len(resp.Errors))
	for _, s := range resp.Errors {
		errs = append(errs, errors.New(s))
	}
	return out, errs, nil
}

// recordFromMap converts the server's map-shaped record back into the typed
// snoozetypes.Record. We round-trip through JSON because the server emits
// strongly-typed records but the alert endpoint historically returns raw maps
// to accommodate plugin-injected fields.
func recordFromMap(m map[string]any) (snoozetypes.Record, error) {
	raw, err := json.Marshal(m)
	if err != nil {
		return snoozetypes.Record{}, fmt.Errorf("snoozeclient: marshal record map: %w", err)
	}
	var rec snoozetypes.Record
	if err := json.Unmarshal(raw, &rec); err != nil {
		return snoozetypes.Record{}, fmt.Errorf("snoozeclient: decode record: %w", err)
	}
	return rec, nil
}
