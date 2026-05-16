// Bulk increment helper for the SQLite backend.
//
// Mirrors Postgres ``bulk_increment``: for each (search, deltas) pair, find
// the first matching row, ADD the deltas to its data, write back. If no
// match and ``upsert`` is true, insert a fresh row whose payload is
// (search ∪ deltas).
//
// We chose the row-by-row approach over a single ``INSERT … ON CONFLICT
// DO UPDATE SET data = json_patch(data, $delta)`` statement because the
// JSON1 ``json_patch`` performs a deep merge that REPLACES scalar values
// (it is RFC-7396-compatible) rather than ADDING them, so a delta of
// ``{"hits": 1}`` would overwrite hits=42 with hits=1. Row-by-row keeps
// the increment semantics exactly.

package sqlite

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	dbpkg "github.com/snoozeweb/snooze/internal/db"
)

// BulkIncrement applies a batch of (search, deltas) updates inside a single
// IMMEDIATE transaction. Missing rows are inserted only when upsert=true.
func (d *Driver) BulkIncrement(ctx context.Context, collection string, ops []dbpkg.IncrementOp, upsert bool) error {
	if len(ops) == 0 {
		return nil
	}
	if err := d.ensure(ctx, collection); err != nil {
		return err
	}
	tbl, err := tableName(collection)
	if err != nil {
		return err
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var touched []string
	for _, op := range ops {
		cond := searchDictToCondition(op.Search)
		row, err := d.findOneInTx(ctx, tx, collection, tbl, cond)
		if err != nil {
			return err
		}
		if row != nil {
			for k, delta := range op.Deltas {
				switch existing := row.data[k].(type) {
				case float64:
					row.data[k] = existing + float64(delta)
				case int64:
					row.data[k] = existing + delta
				case int:
					row.data[k] = int64(existing) + delta
				case nil:
					row.data[k] = float64(delta)
				default:
					row.data[k] = float64(delta)
				}
			}
			buf, err := json.Marshal(row.data)
			if err != nil {
				return err
			}
			stmt := fmt.Sprintf( //nolint:gosec
				"UPDATE %s SET data = ?, updated_at = strftime('%%Y-%%m-%%dT%%H:%%M:%%fZ','now') WHERE uid = ?",
				quoteIdent(tbl),
			)
			if _, err := tx.ExecContext(ctx, stmt, string(buf), row.uid); err != nil {
				return err
			}
			touched = append(touched, row.uid)
			continue
		}
		if !upsert {
			continue
		}
		// Build a new row from (search ∪ deltas) and insert it.
		doc := make(dbpkg.Document, len(op.Search)+len(op.Deltas)+1)
		for k, v := range op.Search {
			doc[k] = v
		}
		for k, v := range op.Deltas {
			doc[k] = float64(v)
		}
		if _, ok := doc["uid"].(string); !ok {
			doc["uid"] = uuid.NewString()
		}
		uid := doc["uid"].(string)
		if err := d.insertRow(ctx, tx, tbl, uid, doc); err != nil {
			return err
		}
		touched = append(touched, uid)
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if len(touched) > 0 {
		d.publishMutation(ctx, collection, "write", touched)
	}
	return nil
}
