// Package sqlite is the embedded SQLite implementation of db.Driver, backed
// by the pure-Go modernc.org/sqlite engine and the JSON1 extension.
//
// SQLite is a single-instance backend: there's no cross-process replication,
// so Watcher() returns an in-process channel bus (see bus.go). The schema
// shape mirrors the Postgres backend (snooze_<collection>(uid, data, seq,
// updated_at)) so the higher layers don't branch on driver kind.
package sqlite

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	mcsqlite "modernc.org/sqlite"

	"github.com/google/uuid"

	"github.com/snoozeweb/snooze/internal/condition"
	dbpkg "github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/syncer"
)

// Config configures the SQLite driver.
//
// Path can be a filesystem path or one of SQLite's URI forms (":memory:",
// "file::memory:?cache=shared"). BusyTimeoutMS defaults to 5000.
type Config struct {
	Path          string
	BusyTimeoutMS int
}

// Driver implements db.Driver against modernc.org/sqlite using JSON1.
type Driver struct {
	db     *sql.DB
	cfg    Config
	cache  *schemaCache
	bus    *inprocBus
	closed bool

	// searchFields is the per-collection SEARCH field registry. Populated
	// lazily by CreateIndex (matches the Mongo/Postgres semantics).
	searchFields sync.Map // collection -> []string
}

// New opens (or creates) the SQLite database at cfg.Path and returns a
// Driver. WAL mode, NORMAL synchronous, foreign keys and the configured
// busy timeout are applied via DSN _pragma directives.
func New(ctx context.Context, cfg Config) (*Driver, error) {
	if cfg.Path == "" {
		return nil, errors.New("sqlite: Path is required")
	}
	if cfg.BusyTimeoutMS <= 0 {
		cfg.BusyTimeoutMS = 5000
	}

	// Register the regexp UDF exactly once for the lifetime of the
	// process. RegisterFunction returns an error if the name is already
	// taken, which is the normal case from the second New onward; that's
	// not fatal because the function is process-global.
	registerRegexpOnce.Do(func() {
		_ = mcsqlite.RegisterDeterministicScalarFunction(
			"regexp",
			2,
			regexpUDF,
		)
	})

	db, err := sql.Open("sqlite", buildDSN(cfg))
	if err != nil {
		return nil, fmt.Errorf("sqlite: open: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("sqlite: ping: %w", err)
	}

	// Conservative limits: SQLite serialises writers regardless, and WAL
	// gives us free concurrent readers. The default sql.DB pool is fine,
	// but we keep idle connections bounded so we don't pin file handles
	// on long-running daemons.
	db.SetMaxIdleConns(4)
	db.SetConnMaxIdleTime(5 * time.Minute)

	d := &Driver{
		db:    db,
		cfg:   cfg,
		cache: newSchemaCache(),
		bus:   newInprocBus(),
	}
	return d, nil
}

// registerRegexpOnce gates the process-global UDF registration so multiple
// Driver instances share a single registration.
var registerRegexpOnce sync.Once

// regexpUDF backs the SQL “regexp(pattern, value)“ function. SQLite's
// “column REGEXP 'pat'“ desugars to regexp('pat', column), so the first
// argument is always the pattern. We compile case-insensitively and memoize
// in the per-Driver cache via a module-level map (UDFs are registered once
// per process, so a single shared cache is correct).
//
// Returns SQL NULL on a nil value, a compile error on an invalid pattern.
func regexpUDF(_ *mcsqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("regexp: expected 2 arguments, got %d", len(args))
	}
	if args[0] == nil || args[1] == nil {
		return int64(0), nil
	}
	pattern, ok := args[0].(string)
	if !ok {
		pattern = fmt.Sprint(args[0])
	}
	var value string
	switch v := args[1].(type) {
	case string:
		value = v
	case []byte:
		value = string(v)
	default:
		value = fmt.Sprint(v)
	}
	re, err := getRegexp(pattern)
	if err != nil {
		return nil, err
	}
	if re.MatchString(value) {
		return int64(1), nil
	}
	return int64(0), nil
}

// regexpCache is the process-global compile cache for the regexp UDF.
var regexpCache sync.Map // pattern -> *regexp.Regexp

func getRegexp(pattern string) (*regexp.Regexp, error) {
	if v, ok := regexpCache.Load(pattern); ok {
		return v.(*regexp.Regexp), nil
	}
	// Force case-insensitive to match the MATCHES/CONTAINS Python semantics.
	re, err := regexp.Compile("(?i)" + pattern)
	if err != nil {
		return nil, fmt.Errorf("regexp: compile %q: %w", pattern, err)
	}
	actual, _ := regexpCache.LoadOrStore(pattern, re)
	return actual.(*regexp.Regexp), nil
}

// buildDSN produces a modernc.org/sqlite connection string from cfg.
//
// We always layer WAL, NORMAL sync, foreign keys, and the busy timeout. The
// caller-provided Path may already contain its own query string (e.g.
// "file::memory:?cache=shared"); in that case we append pragmas with `&`,
// otherwise with `?`.
func buildDSN(cfg Config) string {
	pragmas := []string{
		"_pragma=journal_mode(WAL)",
		fmt.Sprintf("_pragma=busy_timeout(%d)", cfg.BusyTimeoutMS),
		"_pragma=synchronous(NORMAL)",
		"_pragma=foreign_keys(on)",
	}
	sep := "?"
	if strings.Contains(cfg.Path, "?") {
		sep = "&"
	}
	return cfg.Path + sep + strings.Join(pragmas, "&")
}

// Close releases the underlying *sql.DB and the in-process bus. Idempotent.
func (d *Driver) Close() error {
	if d.closed {
		return nil
	}
	d.closed = true
	_ = d.bus.Close()
	return d.db.Close()
}

// Watcher returns the per-driver event bus. For SQLite this is an
// in-process channel bus shared by every consumer in the binary.
func (d *Driver) Watcher() syncer.Bus { return d.bus }

// ------------------------------------------------------------------ //
// Convert / search-fields registration                               //
// ------------------------------------------------------------------ //

// Convert compiles a condition.Cond into a DriverQuery that downstream
// callers can pass back unchanged. For SQLite the compiled form is just
// the SQL where clause plus its bound args, ready for splicing under
// `WHERE`.
func (d *Driver) Convert(_ context.Context, cond condition.Cond, searchFields []string) (dbpkg.DriverQuery, error) {
	clause, args, err := compile(cond, searchFields)
	if err != nil {
		return nil, err
	}
	return compiledQuery{where: clause, args: args}, nil
}

// CreateIndex registers a collection's SEARCH field list AND creates a
// JSON-extract expression index per field so equality and ordering on
// hot fields skip a full table scan. Mirrors the Mongo/Postgres
// semantics: the index list is metadata only as far as SEARCH goes.
func (d *Driver) CreateIndex(ctx context.Context, collection string, fields []string) error {
	if err := d.ensure(ctx, collection); err != nil {
		return err
	}
	d.searchFields.Store(collection, append([]string(nil), fields...))
	tbl, err := tableName(collection)
	if err != nil {
		return err
	}
	for _, f := range fields {
		ident := sanitizeIdent(f)
		idx := fmt.Sprintf("idx_%s_%s", tbl, ident)
		stmt := fmt.Sprintf(
			"CREATE INDEX IF NOT EXISTS %s ON %s(json_extract(data, '$.%s'))",
			quoteIdent(idx), quoteIdent(tbl), escapeJSONPath(f),
		)
		if _, err := d.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("sqlite: create index %s: %w", idx, err)
		}
	}
	return nil
}

// ListCollections returns every collection backed by a snooze_* table in
// the connected database. Names are reverse-mapped (snooze_a__b -> a.b).
func (d *Driver) ListCollections(ctx context.Context) ([]string, error) {
	rows, err := d.db.QueryContext(ctx,
		"SELECT name FROM sqlite_master WHERE type='table' AND name LIKE 'snooze_%'")
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, collectionFromTableName(name))
	}
	return out, rows.Err()
}

// Drop deletes a collection's backing table and forgets it from the
// per-process schema cache.
func (d *Driver) Drop(ctx context.Context, collection string) error {
	tbl, err := tableName(collection)
	if err != nil {
		return err
	}
	_, err = d.db.ExecContext(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", quoteIdent(tbl)))
	if err != nil {
		return err
	}
	d.cache.forget(collection)
	return nil
}

// Backup writes one JSON file per collection into dir. The file name is
// "<collection>.json"; collections in exclude are skipped.
func (d *Driver) Backup(ctx context.Context, dir string, exclude []string) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	skip := make(map[string]struct{}, len(exclude))
	for _, c := range exclude {
		skip[c] = struct{}{}
	}
	cols, err := d.ListCollections(ctx)
	if err != nil {
		return err
	}
	for _, c := range cols {
		if _, ok := skip[c]; ok {
			continue
		}
		docs, err := d.dumpCollection(ctx, c)
		if err != nil {
			return err
		}
		buf, err := json.Marshal(docs)
		if err != nil {
			return err
		}
		path := filepath.Join(dir, c+".json")
		if err := os.WriteFile(path, buf, 0o600); err != nil {
			return err
		}
	}
	return nil
}

func (d *Driver) dumpCollection(ctx context.Context, collection string) ([]dbpkg.Document, error) {
	tbl, err := tableName(collection)
	if err != nil {
		return nil, err
	}
	rows, err := d.db.QueryContext(ctx, fmt.Sprintf("SELECT data FROM %s", quoteIdent(tbl)))
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck
	var out []dbpkg.Document
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var doc dbpkg.Document
		if err := json.Unmarshal([]byte(raw), &doc); err != nil {
			return nil, err
		}
		out = append(out, doc)
	}
	return out, rows.Err()
}

// ------------------------------------------------------------------ //
// Search / GetOne                                                    //
// ------------------------------------------------------------------ //

// Search returns documents matching cond, ordered and paginated per page.
// When the collection does not exist yet the result is empty rather than
// an error, matching the Mongo/Postgres behaviour.
func (d *Driver) Search(ctx context.Context, collection string, cond condition.Cond, page dbpkg.Page) ([]dbpkg.Document, int, error) {
	exists, err := d.collectionExists(ctx, collection)
	if err != nil {
		return nil, 0, err
	}
	if !exists {
		return nil, 0, nil
	}
	tbl, err := tableName(collection)
	if err != nil {
		return nil, 0, err
	}
	where, args, err := d.compileWith(collection, cond)
	if err != nil {
		return nil, 0, err
	}

	var total int
	countQ := fmt.Sprintf("SELECT count(*) FROM %s WHERE %s", quoteIdent(tbl), where) //nolint:gosec
	if err := d.db.QueryRowContext(ctx, countQ, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("sqlite: search count: %w", err)
	}

	q := fmt.Sprintf("SELECT data FROM %s WHERE %s", quoteIdent(tbl), where) //nolint:gosec
	q += " " + renderOrderBy(page)
	if page.OnlyOne {
		q += " LIMIT 1"
	} else if page.PerPage > 0 {
		pageNb := page.PageNb
		if pageNb < 1 {
			pageNb = 1
		}
		q += fmt.Sprintf(" LIMIT %d OFFSET %d", page.PerPage, (pageNb-1)*page.PerPage)
	}

	rows, err := d.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("sqlite: search: %w", err)
	}
	defer rows.Close() //nolint:errcheck
	var docs []dbpkg.Document
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, 0, err
		}
		var doc dbpkg.Document
		if err := json.Unmarshal([]byte(raw), &doc); err != nil {
			return nil, 0, err
		}
		docs = append(docs, doc)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if page.OnlyOne {
		if len(docs) == 0 {
			return nil, 0, nil
		}
		return docs[:1], 1, nil
	}
	return docs, total, nil
}

// GetOne returns the first document matching the equality-only search dict,
// or ErrNotFound when nothing matches. A nil/empty match returns the first
// row in natural insertion order.
func (d *Driver) GetOne(ctx context.Context, collection string, match dbpkg.Document) (dbpkg.Document, error) {
	cond := searchDictToCondition(match)
	docs, _, err := d.Search(ctx, collection, cond, dbpkg.Page{OnlyOne: true})
	if err != nil {
		return nil, err
	}
	if len(docs) == 0 {
		return nil, dbpkg.ErrNotFound
	}
	return docs[0], nil
}

// ------------------------------------------------------------------ //
// CRUD                                                               //
// ------------------------------------------------------------------ //

// Write applies the Python BackendDB.write semantics: branch on uid presence,
// primary-key lookup, duplicate policy, constant-field guards. Returns the
// uids classified into added/updated/replaced/rejected.
func (d *Driver) Write(ctx context.Context, collection string, docs []dbpkg.Document, opts dbpkg.WriteOptions) (dbpkg.WriteResult, error) {
	if err := d.ensure(ctx, collection); err != nil {
		return dbpkg.WriteResult{}, err
	}
	tbl, err := tableName(collection)
	if err != nil {
		return dbpkg.WriteResult{}, err
	}

	updateTime := opts.UpdateTime
	// Default to true unless explicitly set to false. Python default is true.
	// The WriteOptions zero-value gives false; we can't distinguish set vs
	// unset here without a *bool, so we follow the field as supplied.

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return dbpkg.WriteResult{}, err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	var result dbpkg.WriteResult
	for _, raw := range docs {
		doc := cloneDoc(raw)
		delete(doc, "_old")
		delete(doc, "_id")
		if updateTime {
			doc["date_epoch"] = float64(time.Now().Unix())
		}

		var primaryRow *existingRow
		if len(opts.Primary) > 0 && allPrimaryPresent(doc, opts.Primary) {
			row, err := d.findOneInTx(ctx, tx, collection, tbl, primaryCondition(doc, opts.Primary))
			if err != nil {
				return result, err
			}
			primaryRow = row
		}

		if uid, ok := doc["uid"].(string); ok && uid != "" {
			existing, err := d.findOneInTx(ctx, tx, collection, tbl, condition.Equals("uid", uid))
			if err != nil {
				return result, err
			}
			if existing == nil {
				doc["error"] = fmt.Sprintf("UID %s not found. Skipping...", uid)
				result.Rejected = append(result.Rejected, dbpkg.Rejection{UID: uid, Reason: doc["error"].(string), Payload: doc})
				continue
			}
			if primaryRow != nil && primaryRow.uid != uid {
				doc["error"] = fmt.Sprintf("Found another document with same primary %v; UID differs, cannot update", opts.Primary)
				result.Rejected = append(result.Rejected, dbpkg.Rejection{UID: uid, Reason: doc["error"].(string), Payload: doc})
				continue
			}
			if violatesConstant(existing.data, doc, opts.Constant) {
				doc["error"] = fmt.Sprintf("Existing uid %s differs on constant fields %v", uid, opts.Constant)
				result.Rejected = append(result.Rejected, dbpkg.Rejection{UID: uid, Reason: doc["error"].(string), Payload: doc})
				continue
			}
			if opts.DuplicatePolicy == "replace" {
				if err := d.replaceRow(ctx, tx, tbl, uid, doc, updateTime); err != nil {
					return result, err
				}
				result.Replaced = append(result.Replaced, uid)
			} else {
				if err := d.updateRow(ctx, tx, tbl, uid, doc, updateTime); err != nil {
					return result, err
				}
				result.Updated = append(result.Updated, uid)
			}
			doc["_old"] = existing.data
			continue
		}

		if len(opts.Primary) > 0 {
			if primaryRow != nil {
				if violatesConstant(primaryRow.data, doc, opts.Constant) {
					doc["error"] = fmt.Sprintf("Existing primary %v differs on constant fields %v", opts.Primary, opts.Constant)
					result.Rejected = append(result.Rejected, dbpkg.Rejection{UID: primaryRow.uid, Reason: doc["error"].(string), Payload: doc})
					continue
				}
				switch opts.DuplicatePolicy {
				case "insert":
					newUID := ensureUID(doc)
					if err := d.insertRow(ctx, tx, tbl, newUID, doc); err != nil {
						return result, err
					}
					result.Added = append(result.Added, newUID)
				case "reject":
					doc["error"] = fmt.Sprintf("Another object exists with the same %v", opts.Primary)
					result.Rejected = append(result.Rejected, dbpkg.Rejection{UID: primaryRow.uid, Reason: doc["error"].(string), Payload: doc})
				case "replace":
					target := primaryRow.uid
					if target == "" {
						target = uuid.NewString()
					}
					doc["uid"] = target
					if err := d.replaceRow(ctx, tx, tbl, target, doc, updateTime); err != nil {
						return result, err
					}
					result.Replaced = append(result.Replaced, target)
				default: // "update" or empty
					if err := d.updateRow(ctx, tx, tbl, primaryRow.uid, doc, updateTime); err != nil {
						return result, err
					}
					result.Updated = append(result.Updated, primaryRow.uid)
				}
				doc["_old"] = primaryRow.data
				continue
			}
			// No existing primary match -> insert.
			newUID := ensureUID(doc)
			if err := d.insertRow(ctx, tx, tbl, newUID, doc); err != nil {
				return result, err
			}
			result.Added = append(result.Added, newUID)
			continue
		}

		// No primary, no uid -> straight insert.
		newUID := ensureUID(doc)
		if err := d.insertRow(ctx, tx, tbl, newUID, doc); err != nil {
			return result, err
		}
		result.Added = append(result.Added, newUID)
	}

	if err := tx.Commit(); err != nil {
		return result, err
	}
	d.publishMutation(ctx, collection, "write", append(append(append([]string{}, result.Added...), result.Updated...), result.Replaced...))
	return result, nil
}

// ReplaceOne replaces the first document matching “match“ with “doc“,
// upserting if no match exists. Returns 1 if a matching row was found,
// 0 if the call performed an insert.
func (d *Driver) ReplaceOne(ctx context.Context, collection string, match dbpkg.Document, doc dbpkg.Document, updateTime bool) (int, error) {
	if err := d.ensure(ctx, collection); err != nil {
		return 0, err
	}
	tbl, err := tableName(collection)
	if err != nil {
		return 0, err
	}
	newObj := cloneDoc(doc)
	delete(newObj, "_id")
	for k, v := range match {
		newObj[k] = v
	}
	if updateTime {
		newObj["date_epoch"] = float64(time.Now().Unix())
	}
	cond := searchDictToCondition(match)
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	row, err := d.findOneInTx(ctx, tx, collection, tbl, cond)
	if err != nil {
		return 0, err
	}
	matched := 0
	var uid string
	if row != nil {
		uid = row.uid
		if _, ok := newObj["uid"].(string); !ok {
			newObj["uid"] = uid
		}
		if err := d.replaceRow(ctx, tx, tbl, uid, newObj, updateTime); err != nil {
			return 0, err
		}
		matched = 1
	} else {
		uid = ensureUID(newObj)
		if err := d.insertRow(ctx, tx, tbl, uid, newObj); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	d.publishMutation(ctx, collection, "replace", []string{uid})
	return matched, nil
}

// UpdateOne shallow-merges patch into the document with the given uid,
// inserting it if missing (upsert).
func (d *Driver) UpdateOne(ctx context.Context, collection, uid string, patch dbpkg.Document, updateTime bool) error {
	if err := d.ensure(ctx, collection); err != nil {
		return err
	}
	tbl, err := tableName(collection)
	if err != nil {
		return err
	}
	merged := cloneDoc(patch)
	delete(merged, "_id")
	if updateTime {
		merged["date_epoch"] = float64(time.Now().Unix())
	}
	if _, ok := merged["uid"].(string); !ok {
		merged["uid"] = uid
	}

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	row, err := d.findOneInTx(ctx, tx, collection, tbl, condition.Equals("uid", uid))
	if err != nil {
		return err
	}
	if row == nil {
		if err := d.insertRow(ctx, tx, tbl, uid, merged); err != nil {
			return err
		}
	} else {
		if err := d.updateRow(ctx, tx, tbl, uid, merged, updateTime); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	d.publishMutation(ctx, collection, "update", []string{uid})
	return nil
}

// Delete removes every document matching cond and returns the count. An
// empty condition with force=false is refused (returns 0, no error) to
// match the Python safety guard.
func (d *Driver) Delete(ctx context.Context, collection string, cond condition.Cond, force bool) (int, error) {
	exists, err := d.collectionExists(ctx, collection)
	if err != nil {
		return 0, err
	}
	if !exists {
		return 0, nil
	}
	if cond.IsZero() && !force {
		return 0, nil
	}
	tbl, err := tableName(collection)
	if err != nil {
		return 0, err
	}
	where, args, err := d.compileWith(collection, cond)
	if err != nil {
		return 0, err
	}

	// Capture uids first for the bus notification, then delete.
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	q := fmt.Sprintf("SELECT uid FROM %s WHERE %s", quoteIdent(tbl), where) //nolint:gosec
	rows, err := tx.QueryContext(ctx, q, args...)
	if err != nil {
		return 0, err
	}
	var uids []string
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			_ = rows.Close()
			return 0, err
		}
		uids = append(uids, uid)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(uids) == 0 {
		return 0, tx.Commit()
	}
	delQ := fmt.Sprintf("DELETE FROM %s WHERE %s", quoteIdent(tbl), where) //nolint:gosec
	res, err := tx.ExecContext(ctx, delQ, args...)
	if err != nil {
		return 0, err
	}
	affected, _ := res.RowsAffected()
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	d.publishMutation(ctx, collection, "delete", uids)
	return int(affected), nil
}

// ------------------------------------------------------------------ //
// Increments / list operations                                       //
// ------------------------------------------------------------------ //

// IncMany adds delta to “field“ on every document matching cond. The field
// is read as a JSON number; missing values are treated as 0.
func (d *Driver) IncMany(ctx context.Context, collection, field string, cond condition.Cond, delta int64) (int, error) {
	if err := d.ensure(ctx, collection); err != nil {
		return 0, err
	}
	tbl, err := tableName(collection)
	if err != nil {
		return 0, err
	}
	where, args, err := d.compileWith(collection, cond)
	if err != nil {
		return 0, err
	}
	path := "$." + escapeJSONPath(field)
	q := fmt.Sprintf( //nolint:gosec
		"UPDATE %s SET data = json_set(data, '%s', "+
			"COALESCE(CAST(json_extract(data, '%s') AS INTEGER), 0) + ?), "+
			"updated_at = strftime('%%Y-%%m-%%dT%%H:%%M:%%fZ','now') WHERE %s",
		quoteIdent(tbl), path, path, where,
	)
	allArgs := append([]any{delta}, args...)
	res, err := d.db.ExecContext(ctx, q, allArgs...)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// SetFields overwrites the named fields on every document matching cond.
func (d *Driver) SetFields(ctx context.Context, collection string, fields dbpkg.Document, cond condition.Cond) (int, error) {
	return d.updateViaPython(ctx, collection, cond, func(doc dbpkg.Document) bool {
		for k, v := range fields {
			doc[k] = v
		}
		return true
	})
}

// UnsetFields removes the named fields from every matching row. The
// read-modify-write callback rewrites the whole `data` payload, so deleting a
// key from the in-memory doc persists (equivalent to json_remove).
func (d *Driver) UnsetFields(ctx context.Context, collection string, fields []string, cond condition.Cond) (int, error) {
	return d.updateViaPython(ctx, collection, cond, func(doc dbpkg.Document) bool {
		changed := false
		for _, k := range fields {
			if _, ok := doc[k]; ok {
				delete(doc, k)
				changed = true
			}
		}
		return changed
	})
}

// AppendList appends values to each named array field on every document
// matching cond. Missing fields are treated as empty lists; non-array
// fields are skipped (matches Mongo's $push semantics).
func (d *Driver) AppendList(ctx context.Context, collection string, fields map[string][]any, cond condition.Cond) (int, error) {
	return d.updateViaPython(ctx, collection, cond, func(doc dbpkg.Document) bool {
		if !listOpApplicable(doc, fields) {
			return false
		}
		for k, values := range fields {
			existing := asAnyList(doc[k])
			doc[k] = append(existing, values...)
		}
		return true
	})
}

// PrependList prepends values to each named array field. Same semantics as
// AppendList, opposite order.
func (d *Driver) PrependList(ctx context.Context, collection string, fields map[string][]any, cond condition.Cond) (int, error) {
	return d.updateViaPython(ctx, collection, cond, func(doc dbpkg.Document) bool {
		if !listOpApplicable(doc, fields) {
			return false
		}
		for k, values := range fields {
			existing := asAnyList(doc[k])
			merged := make([]any, 0, len(values)+len(existing))
			merged = append(merged, values...)
			merged = append(merged, existing...)
			doc[k] = merged
		}
		return true
	})
}

// RemoveList drops the given values from each named array field. Returns
// the number of rows actually changed.
func (d *Driver) RemoveList(ctx context.Context, collection string, fields map[string][]any, cond condition.Cond) (int, error) {
	return d.updateViaPython(ctx, collection, cond, func(doc dbpkg.Document) bool {
		if !listOpApplicable(doc, fields) {
			return false
		}
		changed := false
		for k, values := range fields {
			existing := asAnyList(doc[k])
			drop := make(map[string]struct{}, len(values))
			for _, v := range values {
				drop[fmt.Sprint(v)] = struct{}{}
			}
			out := existing[:0:0]
			for _, item := range existing {
				if _, found := drop[fmt.Sprint(item)]; !found {
					out = append(out, item)
				}
			}
			if len(out) != len(existing) {
				doc[k] = out
				changed = true
			}
		}
		return changed
	})
}

func (d *Driver) updateViaPython(ctx context.Context, collection string, cond condition.Cond, mutate func(dbpkg.Document) bool) (int, error) {
	if err := d.ensure(ctx, collection); err != nil {
		return 0, err
	}
	tbl, err := tableName(collection)
	if err != nil {
		return 0, err
	}
	where, args, err := d.compileWith(collection, cond)
	if err != nil {
		return 0, err
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	q := fmt.Sprintf("SELECT uid, data FROM %s WHERE %s", quoteIdent(tbl), where) //nolint:gosec
	rows, err := tx.QueryContext(ctx, q, args...)
	if err != nil {
		return 0, err
	}
	type pending struct {
		uid  string
		data dbpkg.Document
	}
	var batch []pending
	for rows.Next() {
		var uid, raw string
		if err := rows.Scan(&uid, &raw); err != nil {
			_ = rows.Close()
			return 0, err
		}
		var doc dbpkg.Document
		if err := json.Unmarshal([]byte(raw), &doc); err != nil {
			_ = rows.Close()
			return 0, err
		}
		batch = append(batch, pending{uid: uid, data: doc})
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	updated := 0
	updateStmt := fmt.Sprintf( //nolint:gosec
		"UPDATE %s SET data = ?, updated_at = strftime('%%Y-%%m-%%dT%%H:%%M:%%fZ','now') WHERE uid = ?",
		quoteIdent(tbl),
	)
	var uids []string
	for _, p := range batch {
		if !mutate(p.data) {
			continue
		}
		buf, err := json.Marshal(p.data)
		if err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx, updateStmt, string(buf), p.uid); err != nil {
			return 0, err
		}
		updated++
		uids = append(uids, p.uid)
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	if updated > 0 {
		d.publishMutation(ctx, collection, "update", uids)
	}
	return updated, nil
}

// ------------------------------------------------------------------ //
// Misc helpers / internals                                           //
// ------------------------------------------------------------------ //

// compiledQuery is the DriverQuery payload returned by Convert. Stored
// here rather than convert.go because it's the wire shape between the
// Driver and itself; callers only see it as DriverQuery.
type compiledQuery struct {
	where string
	args  []any
}

// existingRow is a tiny tuple returned by findOneInTx so write paths can
// branch on uid + payload without re-querying.
type existingRow struct {
	uid  string
	data dbpkg.Document
}

func (d *Driver) findOneInTx(ctx context.Context, tx *sql.Tx, collection, tbl string, cond condition.Cond) (*existingRow, error) {
	where, args, err := d.compileWith(collection, cond)
	if err != nil {
		return nil, err
	}
	q := fmt.Sprintf("SELECT uid, data FROM %s WHERE %s LIMIT 1", quoteIdent(tbl), where) //nolint:gosec
	row := tx.QueryRowContext(ctx, q, args...)
	var uid, raw string
	if err := row.Scan(&uid, &raw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	var data dbpkg.Document
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return nil, err
	}
	return &existingRow{uid: uid, data: data}, nil
}

func (d *Driver) compileWith(collection string, cond condition.Cond) (string, []any, error) {
	var fields []string
	if v, ok := d.searchFields.Load(collection); ok {
		fields = v.([]string)
	}
	return compile(cond, fields)
}

func (d *Driver) insertRow(ctx context.Context, tx *sql.Tx, tbl, uid string, doc dbpkg.Document) error {
	doc["uid"] = uid
	buf, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	// seq is computed inside the statement so it stays monotonic without
	// claiming the PK slot (uid is the PK). The sub-select runs in the
	// same transaction; concurrent writers serialise on the SQLite
	// write lock so two rows never share the same seq.
	stmt := fmt.Sprintf( //nolint:gosec
		"INSERT INTO %s (uid, data, seq) VALUES (?, ?, "+
			"COALESCE((SELECT MAX(seq) FROM %s), 0) + 1)",
		quoteIdent(tbl), quoteIdent(tbl),
	)
	_, err = tx.ExecContext(ctx, stmt, uid, string(buf))
	return err
}

func (d *Driver) replaceRow(ctx context.Context, tx *sql.Tx, tbl, uid string, doc dbpkg.Document, updateTime bool) error {
	doc["uid"] = uid
	buf, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	updatedExpr := "updated_at"
	if updateTime {
		updatedExpr = "strftime('%Y-%m-%dT%H:%M:%fZ','now')"
	}
	_, err = tx.ExecContext(ctx, fmt.Sprintf(
		"INSERT INTO %s (uid, data, seq) VALUES (?, ?, "+
			"COALESCE((SELECT MAX(seq) FROM %s), 0) + 1) "+
			"ON CONFLICT(uid) DO UPDATE SET data = excluded.data, updated_at = %s",
		quoteIdent(tbl), quoteIdent(tbl), updatedExpr,
	), uid, string(buf))
	return err
}

// updateRow merges patch into the existing row's JSON data via json_patch,
// matching the Python "shallow merge" semantics.
func (d *Driver) updateRow(ctx context.Context, tx *sql.Tx, tbl, uid string, patch dbpkg.Document, updateTime bool) error {
	patch["uid"] = uid
	buf, err := json.Marshal(patch)
	if err != nil {
		return err
	}
	updatedExpr := "updated_at"
	if updateTime {
		updatedExpr = "strftime('%Y-%m-%dT%H:%M:%fZ','now')"
	}
	_, err = tx.ExecContext(ctx, fmt.Sprintf(
		"UPDATE %s SET data = json_patch(data, ?), updated_at = %s WHERE uid = ?",
		quoteIdent(tbl), updatedExpr,
	), string(buf), uid)
	return err
}

func (d *Driver) collectionExists(ctx context.Context, collection string) (bool, error) {
	tbl, err := tableName(collection)
	if err != nil {
		return false, err
	}
	var name string
	err = d.db.QueryRowContext(ctx,
		"SELECT name FROM sqlite_master WHERE type='table' AND name = ?", tbl,
	).Scan(&name)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (d *Driver) publishMutation(ctx context.Context, collection, op string, uids []string) {
	_ = d.bus.Publish(ctx, syncer.Event{
		Topic:      "collection." + collection,
		Op:         op,
		Collection: collection,
		UIDs:       append([]string(nil), uids...),
		At:         time.Now(),
	})
}

// renderOrderBy renders the ORDER BY clause for Search. Without an explicit
// field we sort by “seq“ (insertion order); with one, we emit a
// two-level sort that handles numeric-looking text correctly.
func renderOrderBy(page dbpkg.Page) string {
	dir := "ASC"
	if !page.Asc {
		dir = "DESC"
	}
	if page.OrderBy == "" || page.OrderBy == "$natural" {
		return fmt.Sprintf("ORDER BY seq %s", dir)
	}
	expr := pathExpr(page.OrderBy, true)
	return fmt.Sprintf(
		"ORDER BY CASE WHEN %s GLOB '-[0-9]*' OR %s GLOB '[0-9]*' THEN CAST(%s AS REAL) END %s, %s %s",
		expr, expr, expr, dir, expr, dir,
	)
}

// searchDictToCondition turns a {key: value} match into a flat AND of
// equality conditions, matching the Python convention.
func searchDictToCondition(match dbpkg.Document) condition.Cond {
	if len(match) == 0 {
		return condition.Cond{}
	}
	clauses := make([]condition.Cond, 0, len(match))
	for k, v := range match {
		clauses = append(clauses, condition.Equals(k, v))
	}
	if len(clauses) == 1 {
		return clauses[0]
	}
	return condition.And(clauses...)
}

func primaryCondition(doc dbpkg.Document, primary []string) condition.Cond {
	clauses := make([]condition.Cond, 0, len(primary))
	for _, p := range primary {
		clauses = append(clauses, condition.Equals(p, digDoc(doc, strings.Split(p, "."))))
	}
	if len(clauses) == 1 {
		return clauses[0]
	}
	return condition.And(clauses...)
}

// digDoc walks doc by the supplied path components. Returns nil if any
// intermediate value is not a map or is missing.
func digDoc(doc dbpkg.Document, parts []string) any {
	var cur any = doc
	for _, p := range parts {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		v, ok := m[p]
		if !ok {
			return nil
		}
		cur = v
	}
	return cur
}

// allPrimaryPresent returns true when every primary path resolves to a
// non-zero value on doc — same predicate the Python branch uses.
func allPrimaryPresent(doc dbpkg.Document, primary []string) bool {
	for _, p := range primary {
		v := digDoc(doc, strings.Split(p, "."))
		if v == nil {
			return false
		}
		switch x := v.(type) {
		case string:
			if x == "" {
				return false
			}
		case bool:
			if !x {
				return false
			}
		case float64:
			if x == 0 {
				return false
			}
		case int64:
			if x == 0 {
				return false
			}
		case int:
			if x == 0 {
				return false
			}
		}
	}
	return true
}

func violatesConstant(existing, incoming dbpkg.Document, constant []string) bool {
	for _, c := range constant {
		if !equalDeep(existing[c], incoming[c]) {
			// Treat absent-vs-empty-string the way the Python code does.
			if existing[c] == nil && incoming[c] == "" {
				continue
			}
			if existing[c] == "" && incoming[c] == nil {
				continue
			}
			return true
		}
	}
	return false
}

// equalDeep is a panic-safe equality for constant-field change detection.
// Values come from JSON, so they may be uncomparable ([]any, map[string]any)
// and a bare != would panic. Mirrors the mongo/postgres backends' equalDeep.
func equalDeep(a, b any) bool {
	if a == nil || b == nil {
		return a == b
	}
	return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
}

func ensureUID(doc dbpkg.Document) string {
	if uid, ok := doc["uid"].(string); ok && uid != "" {
		return uid
	}
	uid := uuid.NewString()
	doc["uid"] = uid
	return uid
}

func cloneDoc(in dbpkg.Document) dbpkg.Document {
	out := make(dbpkg.Document, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func asAnyList(v any) []any {
	if v == nil {
		return nil
	}
	if l, ok := v.([]any); ok {
		return l
	}
	return nil
}

func listOpApplicable(doc dbpkg.Document, fields map[string][]any) bool {
	for k := range fields {
		v, ok := doc[k]
		if !ok || v == nil {
			continue
		}
		if _, isList := v.([]any); !isList {
			return false
		}
	}
	return true
}

// quoteIdent quotes a SQL identifier with double quotes per the SQL standard
// SQLite supports. Embedded double quotes are doubled. We validate names
// against a strict regex upstream so this is only paranoia.
func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
