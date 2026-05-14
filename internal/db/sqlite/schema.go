// Lazy table bootstrap for the SQLite backend.
//
// Schema choice: mirrors the Postgres backend (snooze_<collection>(uid, data,
// seq, updated_at)) using JSON1 + a CHECK json_valid(data) constraint. We
// CANNOT use ``seq INTEGER PRIMARY KEY AUTOINCREMENT`` because SQLite only
// allows one INTEGER PRIMARY KEY per table and our primary key is the TEXT
// ``uid``. Instead we keep ``seq INTEGER NOT NULL DEFAULT 0`` and set it
// inside every INSERT statement via a sub-select:
//
//	INSERT INTO snooze_x (uid, data, seq) VALUES (
//	  ?, ?, COALESCE((SELECT MAX(seq) FROM snooze_x), 0) + 1
//	)
//
// The sub-select runs inside the same statement (and the same transaction
// in the write path), so seq is monotonic, gap-free between successive
// inserts in a tx, and stable across reads — exactly the Postgres
// BIGSERIAL semantics for the order we care about (Search default ORDER BY).

package sqlite

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
)

// collectionNameRE matches the same character class as the Postgres backend's
// _COLLECTION_RE. Identifiers must start with a letter or underscore; the rest
// is alphanumerics, underscores, or dots (dots are remapped to "__").
var collectionNameRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.]*$`)

// tableName returns the physical table name for a logical collection.
func tableName(collection string) (string, error) {
	if !collectionNameRE.MatchString(collection) {
		return "", fmt.Errorf("sqlite: bad collection name %q", collection)
	}
	return "snooze_" + strings.ReplaceAll(collection, ".", "__"), nil
}

// collectionFromTableName is the inverse of tableName for the snooze_ prefix
// and the “.“ -> “__“ rewrite. Returns "" if the table isn't ours.
func collectionFromTableName(table string) string {
	if !strings.HasPrefix(table, "snooze_") {
		return ""
	}
	return strings.ReplaceAll(table[len("snooze_"):], "__", ".")
}

// schemaCache is a per-Driver memo of which collection tables we've already
// ensured exist. Safe across goroutines.
type schemaCache struct {
	mu    sync.Mutex
	known map[string]struct{}
}

func newSchemaCache() *schemaCache {
	return &schemaCache{known: make(map[string]struct{})}
}

func (s *schemaCache) isKnown(c string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.known[c]
	return ok
}

func (s *schemaCache) markKnown(c string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.known[c] = struct{}{}
}

func (s *schemaCache) forget(c string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.known, c)
}

// ensure creates the per-collection table and supporting indexes if they
// don't already exist. Cheap on the hot path thanks to the cache.
func (d *Driver) ensure(ctx context.Context, collection string) error {
	if d.cache.isKnown(collection) {
		return nil
	}
	tbl, err := tableName(collection)
	if err != nil {
		return err
	}
	createTable := fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s (
			uid TEXT PRIMARY KEY,
			data TEXT NOT NULL CHECK (json_valid(data)),
			seq INTEGER NOT NULL DEFAULT 0,
			updated_at TEXT NOT NULL DEFAULT (strftime('%%Y-%%m-%%dT%%H:%%M:%%fZ','now'))
		)`,
		quoteIdent(tbl),
	)
	createIdx := fmt.Sprintf(
		`CREATE INDEX IF NOT EXISTS %s ON %s (updated_at)`,
		quoteIdent("idx_"+tbl+"_updated_at"), quoteIdent(tbl),
	)
	createSeqIdx := fmt.Sprintf(
		`CREATE INDEX IF NOT EXISTS %s ON %s (seq)`,
		quoteIdent("idx_"+tbl+"_seq"), quoteIdent(tbl),
	)
	for _, s := range []string{createTable, createIdx, createSeqIdx} {
		if _, err := d.db.ExecContext(ctx, s); err != nil {
			return fmt.Errorf("sqlite: ensure %s: %w", tbl, err)
		}
	}
	d.cache.markKnown(collection)
	return nil
}

// sanitizeIdent strips characters that would break a SQL identifier when we
// derive index names from a dotted field path. Result is safe to splice
// after quoteIdent.
func sanitizeIdent(field string) string {
	var b strings.Builder
	for _, r := range field {
		switch {
		case r == '.':
			b.WriteString("__")
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_':
			b.WriteRune(r)
		}
	}
	return b.String()
}
