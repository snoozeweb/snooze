// Package postgres implements the db.Driver interface against PostgreSQL using
// pgx/v5. Collections are materialised as per-collection tables holding a
// single JSONB payload column; collection names are sanitised and dots are
// rewritten so they map to valid SQL identifiers.
package postgres

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	dbpkg "github.com/snoozeweb/snooze/internal/db"
)

// collectionRE matches valid logical collection names. Dots are permitted in
// the logical name (mirroring MongoDB's namespacing) and are rewritten to
// "__" on the wire so the underlying SQL identifier remains valid.
var collectionRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.]*$`)

// tablePrefix is the literal string prepended to every collection table name.
const tablePrefix = "snooze_"

// sanitizeCollection validates the logical collection name and returns the
// physical SQL table name (always lower-cased identifier-safe). It is the
// only place user-controlled collection names are turned into SQL
// identifiers, so every place that interpolates an identifier MUST go
// through this helper.
func sanitizeCollection(collection string) (string, error) {
	if !collectionRE.MatchString(collection) {
		return "", fmt.Errorf("%w: %q", dbpkg.ErrBadCollection, collection)
	}
	return tablePrefix + strings.ReplaceAll(collection, ".", "__"), nil
}

// collectionFromTable is the inverse of sanitizeCollection for the rows we
// own (those that have the snooze_ prefix). Returns "" for foreign tables.
func collectionFromTable(table string) string {
	if !strings.HasPrefix(table, tablePrefix) {
		return ""
	}
	return strings.ReplaceAll(table[len(tablePrefix):], "__", ".")
}

// quoteIdent quotes a SQL identifier safely. Postgres double-quotes use
// "" as an embedded quote, but our sanitized identifiers never contain one,
// so wrapping is sufficient.
func quoteIdent(ident string) string {
	return `"` + strings.ReplaceAll(ident, `"`, `""`) + `"`
}

// schemaCache tracks the collections whose backing table this process has
// already ensured. Concurrent ensures for the same collection are gated so
// only one CREATE-IF-NOT-EXISTS statement is in flight at a time.
type schemaCache struct {
	known sync.Map // collection (logical name) -> *sync.Once
}

func (c *schemaCache) loadOrCreate(collection string) *sync.Once {
	if v, ok := c.known.Load(collection); ok {
		return v.(*sync.Once)
	}
	v, _ := c.known.LoadOrStore(collection, &sync.Once{})
	return v.(*sync.Once)
}

func (c *schemaCache) forget(collection string) {
	c.known.Delete(collection)
}

// ensureCollection lazily creates the per-collection table and its baseline
// indexes. Idempotent and process-wide cached.
func (d *Driver) ensureCollection(ctx context.Context, collection string) (string, error) {
	table, err := sanitizeCollection(collection)
	if err != nil {
		return "", err
	}
	once := d.schema.loadOrCreate(collection)
	var execErr error
	once.Do(func() {
		execErr = createCollectionTable(ctx, d.pool, table)
	})
	if execErr != nil {
		// Clear the once entry so the next call retries the DDL.
		d.schema.forget(collection)
		return "", execErr
	}
	return table, nil
}

// createCollectionTable runs the DDL for one collection. The statements are
// idempotent so concurrent processes racing on the same DB converge.
func createCollectionTable(ctx context.Context, pool *pgxpool.Pool, table string) error {
	qt := quoteIdent(table)
	ginIdx := quoteIdent("idx_" + table + "_data_gin")
	updIdx := quoteIdent("idx_" + table + "_updated_at")

	stmts := []string{
		// uid is the document identifier. data carries the payload. seq is the
		// insertion-order column used as the default ORDER BY. updated_at is
		// touched on every mutation.
		fmt.Sprintf(
			"CREATE TABLE IF NOT EXISTS %s ("+
				"uid TEXT PRIMARY KEY, "+
				"data JSONB NOT NULL, "+
				"seq BIGSERIAL NOT NULL, "+
				"updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp())",
			qt,
		),
		fmt.Sprintf(
			"CREATE INDEX IF NOT EXISTS %s ON %s USING GIN (data jsonb_path_ops)",
			ginIdx, qt,
		),
		fmt.Sprintf(
			"CREATE INDEX IF NOT EXISTS %s ON %s (updated_at)",
			updIdx, qt,
		),
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("postgres: begin schema tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	for _, stmt := range stmts {
		if _, err := tx.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("postgres: ddl %q: %w", stmt, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("postgres: commit schema tx: %w", err)
	}
	return nil
}

// listCollectionTables returns the physical table names for every snooze_*
// table visible in the current search_path.
func listCollectionTables(ctx context.Context, pool *pgxpool.Pool) ([]string, error) {
	rows, err := pool.Query(ctx,
		"SELECT tablename FROM pg_tables "+
			"WHERE schemaname = ANY (current_schemas(false)) "+
			"AND tablename LIKE 'snooze_%'",
	)
	if err != nil {
		return nil, fmt.Errorf("postgres: list tables: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, fmt.Errorf("postgres: scan table name: %w", err)
		}
		if c := collectionFromTable(t); c != "" {
			out = append(out, c)
		}
	}
	return out, rows.Err()
}
