package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/snoozeweb/snooze/internal/condition"
	dbpkg "github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/syncer"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// Config controls how the Postgres driver connects to the database. DSN is
// any libpq-compatible URL or keyword/value string. ApplicationName is set
// on every pooled connection so DB-side dashboards can attribute load.
type Config struct {
	DSN             string
	PoolMin         int
	PoolMax         int
	ApplicationName string
}

// Driver is the Postgres implementation of db.Driver. It owns a pgx pool, a
// per-process schema cache, and a LISTEN/NOTIFY-backed event bus.
type Driver struct {
	pool   *pgxpool.Pool
	schema *schemaCache

	mu           sync.RWMutex
	searchFields map[string][]string

	bus *pgBus

	closeOnce sync.Once
	closed    bool

	// busCancel cancels the bus' parent context on Close.
	busCancel context.CancelFunc
}

// compile-time check that *Driver satisfies the db.Driver contract.
var _ dbpkg.Driver = (*Driver)(nil)

// New connects to Postgres and returns a ready-to-use Driver. The supplied
// context governs the connection establishment; the pool itself lives until
// Close().
func New(ctx context.Context, cfg Config) (*Driver, error) {
	if cfg.PoolMin <= 0 {
		cfg.PoolMin = 1
	}
	if cfg.PoolMax <= 0 {
		cfg.PoolMax = 10
	}

	pcfg, err := pgxpool.ParseConfig(cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("postgres: parse dsn: %w", err)
	}
	pcfg.MinConns = int32(cfg.PoolMin) //nolint:gosec
	pcfg.MaxConns = int32(cfg.PoolMax) //nolint:gosec
	if cfg.ApplicationName != "" {
		if pcfg.ConnConfig.RuntimeParams == nil {
			pcfg.ConnConfig.RuntimeParams = map[string]string{}
		}
		pcfg.ConnConfig.RuntimeParams["application_name"] = cfg.ApplicationName
	}

	pool, err := pgxpool.NewWithConfig(ctx, pcfg)
	if err != nil {
		return nil, fmt.Errorf("postgres: connect: %w", err)
	}
	// Verify we can reach the server before returning.
	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres: ping: %w", err)
	}

	busCtx, busCancel := context.WithCancel(context.Background())
	bus, err := newPgBus(busCtx, pool, cfg)
	if err != nil {
		busCancel()
		pool.Close()
		return nil, fmt.Errorf("postgres: bus: %w", err)
	}

	return &Driver{
		pool:         pool,
		schema:       &schemaCache{},
		searchFields: map[string][]string{},
		bus:          bus,
		busCancel:    busCancel,
	}, nil
}

// Close releases the pool and shuts down the LISTEN goroutine. Idempotent.
func (d *Driver) Close() error {
	d.closeOnce.Do(func() {
		d.mu.Lock()
		d.closed = true
		d.mu.Unlock()
		if d.bus != nil {
			_ = d.bus.Close()
		}
		if d.busCancel != nil {
			d.busCancel()
		}
		if d.pool != nil {
			d.pool.Close()
		}
	})
	return nil
}

// Watcher returns the LISTEN/NOTIFY bus used by the syncer.
func (d *Driver) Watcher() syncer.Bus { return d.bus }

// ---------------------------------------------------------------------------
// Search / query path
// ---------------------------------------------------------------------------

// PreparedQuery renders the condition into an opaque driver-specific query bundle
// that GetOne and Search can consume without redoing the work. Returned
// value is a *PreparedQuery; downstream consumers type-assert.
type PreparedQuery struct {
	SQL    string
	Params []any
}

// Convert returns a PreparedQuery suitable for use under WHERE. The db.Driver
// interface signature carries no collection, so this pre-compilation tool runs
// under platform scope (no tenant injection); callers that know the collection
// go through Search/Delete/etc. which thread it for injection.
func (d *Driver) Convert(ctx context.Context, cond condition.Cond, searchFields []string) (dbpkg.DriverQuery, error) {
	res, err := convert(snoozetypes.WithPlatformScope(ctx), "", cond, searchFields)
	if err != nil {
		return nil, err
	}
	return &PreparedQuery{SQL: res.SQL, Params: res.Params}, nil
}

// Search runs the condition under the collection and returns the matching
// payloads plus the total match count. The total is -1 when only-one mode
// short-circuits the count.
func (d *Driver) Search(ctx context.Context, collection string, cond condition.Cond, page dbpkg.Page) ([]dbpkg.Document, int, error) {
	// Fail-closed before the table-exist check: a scoped collection with
	// neither a tenant nor platform scope must error even when the table does
	// not yet exist (which short-circuits convert below). Tenant injection
	// itself is handled inside convert via TenantScope.
	if _, _, err := dbpkg.TenantScope(ctx, collection); err != nil {
		return nil, 0, fmt.Errorf("postgres: search: %w", err)
	}
	table, err := d.tableIfExists(ctx, collection)
	if err != nil {
		return nil, 0, err
	}
	if table == "" {
		return nil, 0, nil
	}
	res, err := convert(ctx, collection, cond, d.getSearchFields(collection))
	if err != nil {
		return nil, 0, err
	}
	qt := quoteIdent(table)

	// Count first (unless only-one short-circuits it).
	total := 0
	if !page.OnlyOne {
		countSQL := fmt.Sprintf("SELECT count(*) FROM %s WHERE %s", qt, res.SQL)
		row := d.pool.QueryRow(ctx, countSQL, res.Params...)
		if err := row.Scan(&total); err != nil {
			return nil, 0, fmt.Errorf("postgres: count %s: %w", collection, err)
		}
	}

	// Main query.
	q := fmt.Sprintf("SELECT data FROM %s WHERE %s", qt, res.SQL)
	switch {
	case page.OrderBy != "" && page.OrderBy != "$natural":
		q += " " + renderOrderBy(page.OrderBy, page.Asc)
	default:
		// Stable insertion order.
		dir := "ASC"
		if !page.Asc && page.OrderBy != "" {
			dir = "DESC"
		}
		// Default Asc=false means natural insertion order ascending by seq.
		// Callers that want descending pass OrderBy="seq", Asc=false.
		q += " ORDER BY seq " + dir
	}
	switch {
	case page.OnlyOne:
		q += " LIMIT 1"
	case page.PerPage > 0:
		q += " " + renderPagination(page.PerPage, page.PageNb)
	}

	rows, err := d.pool.Query(ctx, q, res.Params...)
	if err != nil {
		return nil, 0, fmt.Errorf("postgres: search %s: %w", collection, err)
	}
	defer rows.Close()
	out := make([]dbpkg.Document, 0)
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, 0, fmt.Errorf("postgres: scan: %w", err)
		}
		doc := dbpkg.Document{}
		if err := json.Unmarshal(raw, &doc); err != nil {
			return nil, 0, fmt.Errorf("postgres: decode: %w", err)
		}
		out = append(out, doc)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("postgres: rows: %w", err)
	}
	if page.OnlyOne {
		total = len(out)
	}
	return out, total, nil
}

// GetOne returns the first row matching the equality conjunction match. The
// match map is converted into an AND-of-equals condition for translation.
func (d *Driver) GetOne(ctx context.Context, collection string, match dbpkg.Document) (dbpkg.Document, error) {
	cond := matchToCond(match)
	docs, _, err := d.Search(ctx, collection, cond, dbpkg.Page{OnlyOne: true})
	if err != nil {
		return nil, err
	}
	if len(docs) == 0 {
		return nil, dbpkg.ErrNotFound
	}
	return docs[0], nil
}

// matchToCond converts a {k: v, ...} match map into an AND-of-equals Cond.
// An empty map evaluates to AlwaysTrue.
func matchToCond(match dbpkg.Document) condition.Cond {
	if len(match) == 0 {
		return condition.Cond{}
	}
	children := make([]condition.Cond, 0, len(match))
	for k, v := range match {
		children = append(children, condition.Equals(k, v))
	}
	if len(children) == 1 {
		return children[0]
	}
	return condition.Cond{Op: condition.OpAnd, Children: children}
}

// ---------------------------------------------------------------------------
// Write path
// ---------------------------------------------------------------------------

// Write upserts the supplied documents according to opts. Returns a
// WriteResult tracking which uids were added/updated/replaced/rejected.
func (d *Driver) Write(ctx context.Context, collection string, docs []dbpkg.Document, opts dbpkg.WriteOptions) (dbpkg.WriteResult, error) {
	table, err := d.ensureCollection(ctx, collection)
	if err != nil {
		return dbpkg.WriteResult{}, err
	}
	qt := quoteIdent(table)

	out := dbpkg.WriteResult{}

	// Tenant injection: resolve once (ctx+collection are constant across docs).
	// The primary-key lookups in findOneUIDByPrimary already fence by tenant via
	// convert -> TenantScope, so we only need to stamp the stored doc here.
	tenantID, injectTenant, tenantErr := dbpkg.TenantScope(ctx, collection)
	if tenantErr != nil {
		return out, fmt.Errorf("postgres: write: %w", tenantErr)
	}

	tx, err := d.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return out, fmt.Errorf("postgres: begin write tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	for _, raw := range docs {
		doc := cloneDoc(raw)
		// Strip Mongo-leaking metadata.
		delete(doc, "_id")
		delete(doc, "_old")
		// Match mongo/sqlite: only stamp date_epoch when the caller opts in.
		// Aggregaterule's throttle relies on date_epoch being preserved across
		// ActionAbortUpdate writes (UpdateTime=false), so a blanket stamp here
		// would collapse the throttle window on every duplicate.
		if opts.UpdateTime {
			doc["date_epoch"] = float64(time.Now().Unix())
		}
		if injectTenant {
			doc["tenant_id"] = tenantID
		}

		var primaryUID string
		if len(opts.Primary) > 0 && allPrimaryPresent(doc, opts.Primary) {
			primaryUID, err = d.findOneUIDByPrimary(ctx, tx, qt, collection, doc, opts.Primary)
			if err != nil {
				return out, err
			}
		}

		// fence is the tenant_id that mergeRow/replaceRow must match on the
		// existing row before overwriting it (empty under platform scope / global
		// collections). It gates the by-uid ON CONFLICT so a uid owned by ANOTHER
		// tenant is never merged into or replaced. [C1]
		fence := ""
		if injectTenant {
			fence = tenantID
		}

		if rawUID, ok := doc["uid"].(string); ok && rawUID != "" {
			// findOneByUID is tenant-scoped: a uid owned by another tenant is
			// invisible here, so the by-uid write falls through to the "uid not
			// found" rejection (matching SQLite) and never touches that row. [C1]
			existing, err := d.findOneByUID(ctx, tx, qt, collection, rawUID)
			if err != nil {
				return out, err
			}
			if existing == nil {
				out.Rejected = append(out.Rejected, dbpkg.Rejection{
					UID: rawUID, Reason: "uid not found", Payload: doc,
				})
				continue
			}
			if primaryUID != "" && primaryUID != rawUID {
				out.Rejected = append(out.Rejected, dbpkg.Rejection{
					UID: rawUID, Reason: "primary key collision with different uid", Payload: doc,
				})
				continue
			}
			if violation := constantViolation(existing, doc, opts.Constant); violation != "" {
				out.Rejected = append(out.Rejected, dbpkg.Rejection{
					UID: rawUID, Reason: violation, Payload: doc,
				})
				continue
			}
			switch opts.DuplicatePolicy {
			case "replace":
				if err := replaceRow(ctx, tx, qt, rawUID, doc, fence); err != nil {
					return out, err
				}
				out.Replaced = append(out.Replaced, rawUID)
			default:
				if err := mergeRow(ctx, tx, qt, rawUID, doc, fence); err != nil {
					return out, err
				}
				out.Updated = append(out.Updated, rawUID)
			}
			continue
		}

		// No uid in payload. primaryUID came from the tenant-scoped
		// findOneUIDByPrimary, so it is always an in-tenant uid; the by-uid lookup
		// and the replace/merge below are fenced to the same tenant for safety.
		if len(opts.Primary) > 0 && primaryUID != "" {
			existing, err := d.findOneByUID(ctx, tx, qt, collection, primaryUID)
			if err != nil {
				return out, err
			}
			if violation := constantViolation(existing, doc, opts.Constant); violation != "" {
				out.Rejected = append(out.Rejected, dbpkg.Rejection{
					Reason: violation, Payload: doc,
				})
				continue
			}
			switch opts.DuplicatePolicy {
			case "insert":
				doc["uid"] = newUID()
				if err := insertRow(ctx, tx, qt, doc); err != nil {
					return out, err
				}
				out.Added = append(out.Added, doc["uid"].(string))
			case "reject":
				out.Rejected = append(out.Rejected, dbpkg.Rejection{
					Reason: "duplicate primary key", Payload: doc,
				})
			case "replace":
				doc["uid"] = primaryUID
				if err := replaceRow(ctx, tx, qt, primaryUID, doc, fence); err != nil {
					return out, err
				}
				out.Replaced = append(out.Replaced, primaryUID)
			default:
				if err := mergeRow(ctx, tx, qt, primaryUID, doc, fence); err != nil {
					return out, err
				}
				out.Updated = append(out.Updated, primaryUID)
			}
			continue
		}

		// Plain insert.
		uid, _ := doc["uid"].(string)
		if uid == "" {
			uid = newUID()
			doc["uid"] = uid
		}
		if err := insertRow(ctx, tx, qt, doc); err != nil {
			return out, err
		}
		out.Added = append(out.Added, uid)
	}

	// Emit a single notify event capturing the touched uids by operation.
	affected := append(append([]string{}, out.Added...), out.Updated...)
	affected = append(affected, out.Replaced...)
	if len(affected) > 0 {
		if err := notifyTx(ctx, tx, collection, "write", affected); err != nil {
			return out, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return out, fmt.Errorf("postgres: commit write: %w", err)
	}
	return out, nil
}

func newUID() string { return uuid.NewString() }

// cloneDoc shallow-copies a Document so we can mutate without disturbing
// the caller's input.
func cloneDoc(in dbpkg.Document) dbpkg.Document {
	out := make(dbpkg.Document, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func allPrimaryPresent(doc dbpkg.Document, primary []string) bool {
	for _, p := range primary {
		if _, ok := digDoc(doc, p); !ok {
			return false
		}
	}
	return true
}

// digDoc returns the value at dotted path p, or (nil, false) if missing.
func digDoc(doc dbpkg.Document, path string) (any, bool) {
	parts := splitDotted(path)
	var cur any = doc
	for _, part := range parts {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		next, ok := m[part]
		if !ok {
			return nil, false
		}
		cur = next
	}
	return cur, true
}

func splitDotted(p string) []string {
	if p == "" {
		return nil
	}
	// Lightweight split; avoid strings.Split to dodge an allocation when
	// there are no dots. Kept simple — collection names with embedded
	// quotes have already been rejected upstream.
	out := []string{}
	start := 0
	for i := 0; i < len(p); i++ {
		if p[i] == '.' {
			out = append(out, p[start:i])
			start = i + 1
		}
	}
	out = append(out, p[start:])
	return out
}

// findOneUIDByPrimary looks up the existing uid (if any) whose payload
// matches the supplied primary keys taken from doc.
func (d *Driver) findOneUIDByPrimary(ctx context.Context, tx pgx.Tx, qt, collection string, doc dbpkg.Document, primary []string) (string, error) {
	children := make([]condition.Cond, 0, len(primary))
	for _, k := range primary {
		v, _ := digDoc(doc, k)
		children = append(children, condition.Equals(k, v))
	}
	cond := condition.And(children...)
	res, err := convert(ctx, collection, cond, d.getSearchFields(collection))
	if err != nil {
		return "", err
	}
	q := fmt.Sprintf("SELECT uid FROM %s WHERE %s LIMIT 1", qt, res.SQL)
	row := tx.QueryRow(ctx, q, res.Params...)
	var uid string
	err = row.Scan(&uid)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("postgres: lookup by primary: %w", err)
	}
	return uid, nil
}

// findOneByUID returns the JSONB payload for uid, or nil if absent. The lookup
// is routed through convert(ctx, collection, ...) so it carries the tenant_id
// predicate for a scoped collection: a uid owned by another tenant is invisible
// here, which is what makes the by-uid Write path fail-closed instead of
// reading/overwriting across the tenant boundary. [C1]
func (d *Driver) findOneByUID(ctx context.Context, tx pgx.Tx, qt, collection, uid string) (dbpkg.Document, error) {
	res, err := convert(ctx, collection, condition.Equals("uid", uid), d.getSearchFields(collection))
	if err != nil {
		return nil, err
	}
	q := fmt.Sprintf("SELECT data FROM %s WHERE %s LIMIT 1", qt, res.SQL)
	row := tx.QueryRow(ctx, q, res.Params...)
	var raw []byte
	err = row.Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("postgres: lookup by uid: %w", err)
	}
	doc := dbpkg.Document{}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("postgres: decode: %w", err)
	}
	return doc, nil
}

// constantViolation reports the first immutable-field whose new value
// differs from the existing one. Returns "" when no violation is found.
func constantViolation(existing, incoming dbpkg.Document, constant []string) string {
	if existing == nil {
		return ""
	}
	for _, k := range constant {
		if !equalDeep(existing[k], incoming[k]) {
			return fmt.Sprintf("constant field %q changed", k)
		}
	}
	return ""
}

// equalDeep is a panic-safe equality for constant-field change detection.
// Values come from JSON, so they may be uncomparable ([]any, map[string]any)
// and a bare != would panic. Mirrors the mongo backend's equalDeep.
func equalDeep(a, b any) bool {
	if a == nil || b == nil {
		return a == b
	}
	return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
}

// insertRow inserts a brand-new row. The caller is responsible for setting
// uid on doc.
func insertRow(ctx context.Context, tx pgx.Tx, qt string, doc dbpkg.Document) error {
	raw, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("postgres: marshal: %w", err)
	}
	q := fmt.Sprintf("INSERT INTO %s (uid, data) VALUES ($1, $2::jsonb)", qt)
	if _, err := tx.Exec(ctx, q, doc["uid"], raw); err != nil {
		return fmt.Errorf("postgres: insert: %w", err)
	}
	return nil
}

// replaceRow performs an UPSERT that overwrites data entirely with doc.
//
// When fence is non-empty the ON CONFLICT DO UPDATE is gated on the existing
// row's tenant_id matching fence (same guard as mergeRow): a uid owned by
// another tenant therefore makes the conflict a no-op, so a foreign-tenant row
// is never overwritten and the global uid PK is never violated. A genuinely
// free uid still inserts the (tenant-stamped) doc. [C1]
func replaceRow(ctx context.Context, tx pgx.Tx, qt, uid string, doc dbpkg.Document, fence string) error {
	raw, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("postgres: marshal: %w", err)
	}
	args := []any{uid, raw}
	where := ""
	if fence != "" {
		where = fmt.Sprintf(" WHERE %s.data->>'tenant_id' = $3", qt)
		args = append(args, fence)
	}
	q := fmt.Sprintf(
		"INSERT INTO %s (uid, data) VALUES ($1, $2::jsonb) "+
			"ON CONFLICT (uid) DO UPDATE SET data = EXCLUDED.data, updated_at = clock_timestamp()%s",
		qt, where,
	)
	if _, err := tx.Exec(ctx, q, args...); err != nil {
		return fmt.Errorf("postgres: replace: %w", err)
	}
	return nil
}

// mergeRow merges doc into the existing payload via the jsonb || operator.
// Top-level keys in doc overwrite those in the existing row.
//
// When fence is non-empty the ON CONFLICT DO UPDATE is gated on the existing
// row's tenant_id matching fence: a uid owned by another tenant therefore makes
// the conflict a no-op (Postgres does not raise when the DO UPDATE WHERE is
// false), so another tenant's row is never merged into and the global uid PK is
// never violated. A genuinely free uid still inserts the (tenant-stamped) doc.
func mergeRow(ctx context.Context, tx pgx.Tx, qt, uid string, doc dbpkg.Document, fence string) error {
	raw, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("postgres: marshal: %w", err)
	}
	args := []any{uid, raw}
	where := ""
	if fence != "" {
		where = fmt.Sprintf(" WHERE %s.data->>'tenant_id' = $3", qt)
		args = append(args, fence)
	}
	q := fmt.Sprintf(
		"INSERT INTO %s (uid, data) VALUES ($1, $2::jsonb) "+
			"ON CONFLICT (uid) DO UPDATE SET data = %s.data || EXCLUDED.data, updated_at = clock_timestamp()%s",
		qt, qt, where,
	)
	if _, err := tx.Exec(ctx, q, args...); err != nil {
		return fmt.Errorf("postgres: merge: %w", err)
	}
	return nil
}

// ReplaceOne replaces the first row matching the supplied filter with doc.
// Inserts a new row if no match (upsert semantics).
func (d *Driver) ReplaceOne(ctx context.Context, collection string, match dbpkg.Document, doc dbpkg.Document, updateTime bool) (int, error) {
	table, err := d.ensureCollection(ctx, collection)
	if err != nil {
		return 0, err
	}
	qt := quoteIdent(table)

	// Tenant injection: resolve once. The find side fences by tenant via
	// convert -> TenantScope, so we only need to stamp the stored doc here
	// (mirrors Write). Fail closed before touching storage.
	tenantID, injectTenant, tenantErr := dbpkg.TenantScope(ctx, collection)
	if tenantErr != nil {
		return 0, fmt.Errorf("postgres: replaceone: %w", tenantErr)
	}

	newDoc := cloneDoc(doc)
	delete(newDoc, "_id")
	for k, v := range match {
		newDoc[k] = v
	}
	if updateTime {
		newDoc["date_epoch"] = float64(time.Now().Unix())
	}
	if injectTenant {
		newDoc["tenant_id"] = tenantID
	}

	cond := matchToCond(match)
	res, err := convert(ctx, collection, cond, d.getSearchFields(collection))
	if err != nil {
		return 0, err
	}

	tx, err := d.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, fmt.Errorf("postgres: begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	q := fmt.Sprintf("SELECT uid FROM %s WHERE %s LIMIT 1", qt, res.SQL)
	var uid string
	err = tx.QueryRow(ctx, q, res.Params...).Scan(&uid)
	matched := 0
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// Upsert path.
		generatedUID, _ := newDoc["uid"].(string)
		if generatedUID == "" {
			generatedUID = newUID()
			newDoc["uid"] = generatedUID
		}
		if err := insertRow(ctx, tx, qt, newDoc); err != nil {
			return 0, err
		}
		uid = generatedUID
	case err != nil:
		return 0, fmt.Errorf("postgres: replaceOne lookup: %w", err)
	default:
		// Replace. The uid was found through the tenant-scoped lookup above, so
		// it is in-tenant; fence the upsert to that tenant for safety.
		if _, ok := newDoc["uid"]; !ok {
			newDoc["uid"] = uid
		}
		fence := ""
		if injectTenant {
			fence = tenantID
		}
		if err := replaceRow(ctx, tx, qt, uid, newDoc, fence); err != nil {
			return 0, err
		}
		matched = 1
	}
	if err := notifyTx(ctx, tx, collection, "replace", []string{uid}); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("postgres: commit replaceOne: %w", err)
	}
	return matched, nil
}

// UpdateOne merges patch into the row identified by uid, inserting a new
// row if uid is unknown (upsert semantics).
func (d *Driver) UpdateOne(ctx context.Context, collection, uid string, patch dbpkg.Document, updateTime bool) error {
	table, err := d.ensureCollection(ctx, collection)
	if err != nil {
		return err
	}
	qt := quoteIdent(table)
	// Tenant injection: resolve once. Fail closed before touching storage.
	tenantID, injectTenant, tenantErr := dbpkg.TenantScope(ctx, collection)
	if tenantErr != nil {
		return fmt.Errorf("postgres: updateone: %w", tenantErr)
	}
	newDoc := cloneDoc(patch)
	delete(newDoc, "_id")
	if updateTime {
		newDoc["date_epoch"] = float64(time.Now().Unix())
	}
	if _, ok := newDoc["uid"]; !ok {
		newDoc["uid"] = uid
	}
	if injectTenant {
		newDoc["tenant_id"] = tenantID
	}
	tx, err := d.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("postgres: begin updateOne: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	// mergeRow upserts on the global uid PK. Under a tenant scope the ON CONFLICT
	// DO UPDATE must be fenced so an existing row owned by ANOTHER tenant is never
	// merged into (the conflict becomes a no-op); a genuinely free uid still
	// inserts a tenant-stamped row.
	fence := ""
	if injectTenant {
		fence = tenantID
	}
	if err := mergeRow(ctx, tx, qt, uid, newDoc, fence); err != nil {
		return err
	}
	if err := notifyTx(ctx, tx, collection, "write", []string{uid}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("postgres: commit updateOne: %w", err)
	}
	return nil
}

// Delete drops every row matching cond. force=true must be set when cond is
// empty, otherwise the call no-ops (mirrors the Python guard rail).
func (d *Driver) Delete(ctx context.Context, collection string, cond condition.Cond, force bool) (int, error) {
	// Fail-closed before the existence / force checks: a scoped collection with a
	// naked context must error, never silently no-op. Tenant injection into the
	// predicate is handled by convert below.
	if _, _, err := dbpkg.TenantScope(ctx, collection); err != nil {
		return 0, fmt.Errorf("postgres: delete: %w", err)
	}
	table, err := d.tableIfExists(ctx, collection)
	if err != nil {
		return 0, err
	}
	if table == "" {
		return 0, nil
	}
	if cond.IsZero() && !force {
		return 0, nil
	}
	qt := quoteIdent(table)
	res, err := convert(ctx, collection, cond, d.getSearchFields(collection))
	if err != nil {
		return 0, err
	}
	q := fmt.Sprintf("DELETE FROM %s WHERE %s RETURNING uid", qt, res.SQL)

	tx, err := d.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, fmt.Errorf("postgres: begin delete: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	rows, err := tx.Query(ctx, q, res.Params...)
	if err != nil {
		return 0, fmt.Errorf("postgres: delete: %w", err)
	}
	var uids []string
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			rows.Close()
			return 0, fmt.Errorf("postgres: scan delete: %w", err)
		}
		uids = append(uids, uid)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("postgres: delete rows: %w", err)
	}
	if len(uids) > 0 {
		if err := notifyTx(ctx, tx, collection, "delete", uids); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("postgres: commit delete: %w", err)
	}
	return len(uids), nil
}

// ---------------------------------------------------------------------------
// Indexing / maintenance
// ---------------------------------------------------------------------------

// CreateIndex registers searchFields for SEARCH and ensures the backing
// table exists. The GIN index on data is created lazily by ensureCollection.
func (d *Driver) CreateIndex(ctx context.Context, collection string, fields []string) error {
	if _, err := d.ensureCollection(ctx, collection); err != nil {
		return err
	}
	d.mu.Lock()
	d.searchFields[collection] = append([]string(nil), fields...)
	d.mu.Unlock()
	return nil
}

// getSearchFields returns the registered search fields for collection.
func (d *Driver) getSearchFields(collection string) []string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.searchFields[collection]
}

// ListCollections returns the logical names of every snooze_-prefixed table
// in the current schema.
func (d *Driver) ListCollections(ctx context.Context) ([]string, error) {
	return listCollectionTables(ctx, d.pool)
}

// Drop removes the per-collection table. Idempotent.
func (d *Driver) Drop(ctx context.Context, collection string) error {
	table, err := sanitizeCollection(collection)
	if err != nil {
		return err
	}
	qt := quoteIdent(table)
	if _, err := d.pool.Exec(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", qt)); err != nil {
		return fmt.Errorf("postgres: drop %s: %w", collection, err)
	}
	d.schema.forget(collection)
	return nil
}

// Backup dumps every collection (subject to exclude) to JSON files at dir.
func (d *Driver) Backup(ctx context.Context, dir string, exclude []string) error {
	excl := map[string]struct{}{}
	for _, c := range exclude {
		excl[c] = struct{}{}
	}
	cols, err := d.ListCollections(ctx)
	if err != nil {
		return err
	}
	for _, c := range cols {
		if _, skip := excl[c]; skip {
			continue
		}
		if err := d.backupSingleCollection(ctx, dir, c); err != nil {
			return err
		}
	}
	return nil
}

// tableIfExists returns the physical table name when the collection's table
// has been created, or "" when it hasn't. Useful for read-side paths that
// shouldn't materialise an empty table just to count zero matches.
func (d *Driver) tableIfExists(ctx context.Context, collection string) (string, error) {
	table, err := sanitizeCollection(collection)
	if err != nil {
		return "", err
	}
	row := d.pool.QueryRow(ctx,
		"SELECT 1 FROM pg_tables WHERE schemaname = ANY (current_schemas(false)) AND tablename = $1",
		table,
	)
	var n int
	err = row.Scan(&n)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("postgres: tableIfExists: %w", err)
	}
	return table, nil
}
