package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/snoozeweb/snooze/internal/condition"
	dbpkg "github.com/snoozeweb/snooze/internal/db"
)

// BulkIncrement applies a batch of (search → delta) increment operations
// inside a single transaction. When upsert is true, search dicts with no
// matching row are inserted with the delta values applied.
//
// The implementation is the row-by-row fallback variant. A single-statement
// CTE form is theoretically possible but defeats the per-row search-by-cond
// resolution that mirrors the Python contract. The row-by-row path is the
// stable default; future optimisation may switch identical-shape batches to
// a CTE.
func (d *Driver) BulkIncrement(ctx context.Context, collection string, ops []dbpkg.IncrementOp, upsert bool) error {
	if len(ops) == 0 {
		return nil
	}
	table, err := d.ensureCollection(ctx, collection)
	if err != nil {
		return err
	}
	qt := quoteIdent(table)
	tx, err := d.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("postgres: begin BulkIncrement: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	affected := make([]string, 0, len(ops))
	for _, op := range ops {
		cond := matchToCond(op.Search)
		res, err := convert(cond, d.getSearchFields(collection))
		if err != nil {
			return err
		}
		// Read the current row (uid + data).
		q := fmt.Sprintf("SELECT uid, data FROM %s WHERE %s LIMIT 1", qt, res.SQL)
		var uid string
		var raw []byte
		err = tx.QueryRow(ctx, q, res.Params...).Scan(&uid, &raw)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			if !upsert {
				continue
			}
			// Compose a fresh row from search keys plus deltas.
			newDoc := dbpkg.Document{}
			for k, v := range op.Search {
				newDoc[k] = v
			}
			for f, dv := range op.Deltas {
				newDoc[f] = dv
			}
			if _, ok := newDoc["uid"].(string); !ok {
				newDoc["uid"] = newUID()
			}
			if err := insertRow(ctx, tx, qt, newDoc); err != nil {
				return err
			}
			affected = append(affected, newDoc["uid"].(string))
		case err != nil:
			return fmt.Errorf("postgres: BulkIncrement lookup: %w", err)
		default:
			doc := dbpkg.Document{}
			if err := json.Unmarshal(raw, &doc); err != nil {
				return fmt.Errorf("postgres: BulkIncrement decode: %w", err)
			}
			for f, dv := range op.Deltas {
				cur := toInt64(doc[f])
				doc[f] = cur + dv
			}
			if err := writeRowData(ctx, tx, qt, uid, doc); err != nil {
				return err
			}
			affected = append(affected, uid)
		}
	}
	if len(affected) > 0 {
		if err := notifyTx(ctx, tx, collection, "write", affected); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("postgres: commit BulkIncrement: %w", err)
	}
	return nil
}

// writeRowData replaces the data column for an existing row.
func writeRowData(ctx context.Context, tx pgx.Tx, qt, uid string, doc dbpkg.Document) error {
	raw, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("postgres: marshal: %w", err)
	}
	q := fmt.Sprintf("UPDATE %s SET data = $1::jsonb, updated_at = clock_timestamp() WHERE uid = $2", qt)
	if _, err := tx.Exec(ctx, q, raw, uid); err != nil {
		return fmt.Errorf("postgres: update row: %w", err)
	}
	return nil
}

// toInt64 coerces a numeric JSON-ish value into int64, treating missing/nil
// as zero. Matches Python's COALESCE(value, 0).
func toInt64(v any) int64 {
	switch n := v.(type) {
	case nil:
		return 0
	case int64:
		return n
	case int:
		return int64(n)
	case int32:
		return int64(n)
	case float64:
		return int64(n)
	case float32:
		return int64(n)
	case json.Number:
		i, _ := n.Int64()
		return i
	}
	return 0
}

// IncMany atomically increments the dotted field for every row matching
// cond by delta. Returns the number of rows touched.
func (d *Driver) IncMany(ctx context.Context, collection, field string, cond condition.Cond, delta int64) (int, error) {
	table, err := d.ensureCollection(ctx, collection)
	if err != nil {
		return 0, err
	}
	qt := quoteIdent(table)
	res, err := convert(cond, d.getSearchFields(collection))
	if err != nil {
		return 0, err
	}
	// Use jsonb_set on a single-segment path; multi-segment increment
	// targets aren't supported by Python either.
	parts := splitDotted(field)
	pathArr := "ARRAY[" + strings.Join(quoteEachLiteral(parts), ",") + "]"
	q := fmt.Sprintf(
		"UPDATE %s SET data = jsonb_set(data, %s, to_jsonb(COALESCE((data#>>%s)::numeric, 0) + %d), true), "+
			"updated_at = clock_timestamp() WHERE %s",
		qt, pathArr, pathArr, delta, res.SQL,
	)
	tag, err := d.pool.Exec(ctx, q, res.Params...)
	if err != nil {
		return 0, fmt.Errorf("postgres: incMany: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

func quoteEachLiteral(parts []string) []string {
	out := make([]string, len(parts))
	for i, p := range parts {
		out[i] = sqlString(p)
	}
	return out
}

// SetFields sets the provided keys on every row matching cond. Returns the
// number of touched rows. Implemented as a read-modify-write to keep parity
// with the Mongo and Python backends (nested-key writes via jsonb_set
// require per-field path arrays).
func (d *Driver) SetFields(ctx context.Context, collection string, fields dbpkg.Document, cond condition.Cond) (int, error) {
	return d.updateRowsViaCallback(ctx, collection, cond, func(doc dbpkg.Document) bool {
		for k, v := range fields {
			doc[k] = v
		}
		return true
	})
}

// AppendList appends each value list to the named field on every match.
// Skips rows whose target field is already non-list (mirrors Mongo's
// silent-no-op semantics).
func (d *Driver) AppendList(ctx context.Context, collection string, fields map[string][]any, cond condition.Cond) (int, error) {
	return d.updateRowsViaCallback(ctx, collection, cond, func(doc dbpkg.Document) bool {
		if !listOpApplicable(doc, fields) {
			return false
		}
		for k, values := range fields {
			existing := asAnySlice(doc[k])
			doc[k] = append(append([]any{}, existing...), values...)
		}
		return true
	})
}

// PrependList is AppendList with order reversed.
func (d *Driver) PrependList(ctx context.Context, collection string, fields map[string][]any, cond condition.Cond) (int, error) {
	return d.updateRowsViaCallback(ctx, collection, cond, func(doc dbpkg.Document) bool {
		if !listOpApplicable(doc, fields) {
			return false
		}
		for k, values := range fields {
			existing := asAnySlice(doc[k])
			merged := make([]any, 0, len(existing)+len(values))
			merged = append(merged, values...)
			merged = append(merged, existing...)
			doc[k] = merged
		}
		return true
	})
}

// RemoveList drops every occurrence of the supplied values from the named
// list field. No-op on rows where the field isn't a list.
func (d *Driver) RemoveList(ctx context.Context, collection string, fields map[string][]any, cond condition.Cond) (int, error) {
	return d.updateRowsViaCallback(ctx, collection, cond, func(doc dbpkg.Document) bool {
		if !listOpApplicable(doc, fields) {
			return false
		}
		changed := false
		for k, values := range fields {
			existing := asAnySlice(doc[k])
			filtered := existing[:0]
			for _, item := range existing {
				if !containsEqual(values, item) {
					filtered = append(filtered, item)
				}
			}
			if len(filtered) != len(existing) {
				doc[k] = filtered
				changed = true
			}
		}
		return changed
	})
}

// updateRowsViaCallback reads every row matching cond, calls mutate on the
// decoded data, and writes it back if mutate returns true. Returns the
// number of touched rows.
func (d *Driver) updateRowsViaCallback(ctx context.Context, collection string, cond condition.Cond, mutate func(dbpkg.Document) bool) (int, error) {
	table, err := d.ensureCollection(ctx, collection)
	if err != nil {
		return 0, err
	}
	qt := quoteIdent(table)
	res, err := convert(cond, d.getSearchFields(collection))
	if err != nil {
		return 0, err
	}

	tx, err := d.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, fmt.Errorf("postgres: begin update tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	q := fmt.Sprintf("SELECT uid, data FROM %s WHERE %s", qt, res.SQL)
	rows, err := tx.Query(ctx, q, res.Params...)
	if err != nil {
		return 0, fmt.Errorf("postgres: select rows: %w", err)
	}
	type pair struct {
		uid string
		doc dbpkg.Document
	}
	var matched []pair
	for rows.Next() {
		var uid string
		var raw []byte
		if err := rows.Scan(&uid, &raw); err != nil {
			rows.Close()
			return 0, fmt.Errorf("postgres: scan rows: %w", err)
		}
		doc := dbpkg.Document{}
		if err := json.Unmarshal(raw, &doc); err != nil {
			rows.Close()
			return 0, fmt.Errorf("postgres: decode row: %w", err)
		}
		matched = append(matched, pair{uid: uid, doc: doc})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("postgres: rows iter: %w", err)
	}

	updatedUIDs := make([]string, 0, len(matched))
	for _, p := range matched {
		if !mutate(p.doc) {
			continue
		}
		if err := writeRowData(ctx, tx, qt, p.uid, p.doc); err != nil {
			return 0, err
		}
		updatedUIDs = append(updatedUIDs, p.uid)
	}
	if len(updatedUIDs) > 0 {
		if err := notifyTx(ctx, tx, collection, "write", updatedUIDs); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("postgres: commit update: %w", err)
	}
	return len(updatedUIDs), nil
}

// listOpApplicable returns true when every named field is either missing or
// already a list. Mirrors Mongo's $push/$pull semantics.
func listOpApplicable(doc dbpkg.Document, fields map[string][]any) bool {
	for k := range fields {
		v, ok := doc[k]
		if !ok || v == nil {
			continue
		}
		if _, isSlice := v.([]any); !isSlice {
			return false
		}
	}
	return true
}

func asAnySlice(v any) []any {
	if v == nil {
		return nil
	}
	if l, ok := v.([]any); ok {
		return l
	}
	return nil
}

// containsEqual reports whether values contains an element equal to v.
// Scalars compare via reflect-free fast paths; structured types fall back
// to json equivalence.
func containsEqual(values []any, v any) bool {
	for _, candidate := range values {
		if equalAny(candidate, v) {
			return true
		}
	}
	return false
}

func equalAny(a, b any) bool {
	if a == nil || b == nil {
		return a == b
	}
	// Fast path: directly comparable.
	if isComparable(a) && isComparable(b) && a == b {
		return true
	}
	// Fallback: JSON-encode both. Stable enough for the small list-ops use
	// case and avoids reflect.DeepEqual's surprises with map ordering.
	ja, errA := json.Marshal(a)
	jb, errB := json.Marshal(b)
	if errA != nil || errB != nil {
		return false
	}
	return string(ja) == string(jb)
}

func isComparable(v any) bool {
	switch v.(type) {
	case string, bool,
		int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64:
		return true
	}
	return false
}
