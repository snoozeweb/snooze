package postgres

import (
	"context"
	"crypto/sha1" //nolint:gosec // short, non-cryptographic index-name disambiguator
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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
	// Logger receives the driver's own operational messages (index
	// maintenance). Nil falls back to slog.Default(), which cmd/snooze-server
	// installs via slog.SetDefault before opening the database.
	Logger *slog.Logger
}

// Driver is the Postgres implementation of db.Driver. It owns a pgx pool, a
// per-process schema cache, and a LISTEN/NOTIFY-backed event bus.
type Driver struct {
	pool   *pgxpool.Pool
	schema *schemaCache
	logger *slog.Logger

	mu           sync.RWMutex
	searchFields map[string][]string

	bus *pgBus

	closeOnce sync.Once
	closed    bool

	// busCancel cancels the bus' parent context on Close.
	busCancel context.CancelFunc

	// Index maintenance. Expression-index builds never run on the caller's
	// goroutine (see CreateIndex): they are queued here and executed one at a
	// time by indexWorker, so nothing on a boot path can be stalled by a
	// CREATE INDEX CONCURRENTLY waiting out an old transaction.
	idxMu      sync.Mutex
	idxQueue   []indexJob
	idxBusy    bool // a pass is executing; flushIndexBuilds waits on it
	idxClosing bool
	idxSignal  chan struct{} // capacity 1 "queue non-empty" nudge
	idxDone    chan struct{} // closed when indexWorker returns
	idxCtx     context.Context
	idxCancel  context.CancelFunc
	// idxPassCancel cancels the pass currently executing, so Drop can get an
	// ACCESS EXCLUSIVE lock without deadlocking against a build. It is
	// published in the SAME idxMu window that sets idxBusy (see
	// takeIndexBatch): a state where idxBusy is true and idxPassCancel is nil
	// used to be observable, and abortIndexWork seeing it skipped the cancel
	// and then waited out the whole uncancelled pass.
	idxPassCancel context.CancelFunc
	// idxDropping counts the in-flight Drop calls per collection. A collection
	// in this set accepts no new index jobs (see enqueueIndexJob): without it a
	// CreateIndex landing between abortIndexWork returning idle and DROP TABLE
	// taking its ACCESS EXCLUSIVE lock re-creates exactly the CONCURRENTLY
	// build the abort just removed. It is a count, not a flag, so two
	// overlapping Drops of the same collection cannot un-guard each other.
	idxDropping map[string]int
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

	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	// The index worker's context is process-lifetime, not the caller's: an
	// index build must outlive the boot context that asked for it, and Close
	// is what stops it.
	idxCtx, idxCancel := context.WithCancel(context.Background())
	d := &Driver{
		pool:         pool,
		schema:       &schemaCache{},
		logger:       logger,
		searchFields: map[string][]string{},
		bus:          bus,
		busCancel:    busCancel,
		idxSignal:    make(chan struct{}, 1),
		idxDone:      make(chan struct{}),
		idxCtx:       idxCtx,
		idxCancel:    idxCancel,
		idxDropping:  map[string]int{},
	}
	go d.indexWorker()
	return d, nil
}

// log returns the driver logger, never nil (a zero-value Driver built by a
// test still logs to the process default).
func (d *Driver) log() *slog.Logger {
	if d.logger != nil {
		return d.logger
	}
	return slog.Default()
}

// Close releases the pool and shuts down the LISTEN goroutine. Idempotent.
//
// The index worker is stopped FIRST and awaited. What that wait actually
// bounds, precisely:
//
//   - Cancelling idxCtx cancels the pass context (published atomically with
//     idxBusy, see takeIndexBatch). pgx reacts to a cancelled query context by
//     setting a write/read deadline on the connection's socket, so an
//     in-flight CREATE INDEX CONCURRENTLY unwinds at the client within that
//     deadline instead of running to completion. The wait is therefore
//     normally instant and never longer than one cancelled round-trip.
//   - indexWorkerShutdownWait caps it anyway, for the case a wedged kernel
//     socket does not honour the deadline. Proceeding after the cap is safe
//     *because* the pass runs on its own non-pooled maintenance connection
//     (see runIndexPass): pgxpool.Close waits on its destructor WaitGroup, so
//     if the pass still held a POOLED connection the "bound" would be a
//     fiction — pool.Close would simply block for the rest of the build. It
//     does not, so the worst case after the cap is one orphaned maintenance
//     session that the server-side backend reaps when the process exits (and
//     which drops the advisory lock with it).
//
// The worker still has to stop before the pool does for a different reason:
// runIndexPass reads d.pool.Config() to build its connection.
func (d *Driver) Close() error {
	d.closeOnce.Do(func() {
		d.mu.Lock()
		d.closed = true
		d.mu.Unlock()

		d.idxMu.Lock()
		d.idxClosing = true
		d.idxQueue = nil
		d.idxMu.Unlock()
		if d.idxCancel != nil {
			d.idxCancel()
		}
		if d.idxDone != nil {
			select {
			case <-d.idxDone:
			case <-time.After(indexWorkerShutdownWait):
				// Safe to proceed: the worker's connection is not a pooled
				// one, so pool.Close cannot block on it, and the orphaned
				// session (with its advisory lock) dies with the process.
				d.log().Warn("postgres: index worker did not stop in time; closing the pool anyway",
					"wait", indexWorkerShutdownWait)
			}
		}

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
	// A patch that only bumps this collection's display counters must not
	// pg_notify: the dispatcher writes one per delivery and every node would
	// reload the notification plugin for a number nobody caches. `updateTime`
	// stamps date_epoch, which IS a real change, so it disables the shortcut.
	if updateTime || !dbpkg.IsCounterOnlyPatch(collection, patch) {
		if err := notifyTx(ctx, tx, collection, "write", []string{uid}); err != nil {
			return err
		}
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

// indexJob is one collection's worth of expression-index work, handed to the
// driver-owned background worker by CreateIndex.
type indexJob struct {
	collection string
	table      string // physical (unquoted) table name
	fields     []string
}

// indexKind describes one of the expression indexes maintained per search
// field: the name suffix, the indexed expression, and whether the index is
// partial.
type indexKind struct {
	kind    string // name suffix; "" for the text index
	expr    func(field string) string
	partial bool // add "WHERE <expr> IS NOT NULL"
}

// indexKinds enumerates the expression indexes maintained per search field.
//
// The text kind uses the plain unsuffixed name `idx_<table>_<field>`.
// That is a naming convention only — no released version of the driver ever
// created per-field indexes, so there is nothing on disk to reuse; the name is
// simply the obvious one and the suffix is reserved for the variants.
//
// The numeric kind is PARTIAL. numericExpr is NULL for every row whose text at
// that path is not a plain decimal number, which for a text-only search field
// (`status`, `action`, `notifier`, `host`, …) is every row: a full numeric
// index over `record` would be six all-NULL btrees paid for on every INSERT.
// The partial predicate reuses the SAME expression byte-for-byte, and Postgres
// can prove `expr < $1` implies `expr IS NOT NULL` (the comparison operators
// are strict), so range and equality predicates still match the index while
// an all-non-numeric field costs an empty one.
var indexKinds = []indexKind{
	{kind: "", expr: pathText},
	{kind: "num", expr: numericExpr, partial: true},
}

// indexMaintLockKey names the session-level advisory lock that serialises the
// whole expression-index pass across every server pointed at one database.
const indexMaintLockKey = "snooze_index_maint"

// indexWorkerShutdownWait caps how long Close waits for an in-flight
// CREATE INDEX CONCURRENTLY to notice the cancelled context and unwind. It is
// a backstop, not the mechanism: cancellation is what stops the build (pgx
// puts a deadline on the socket), and the cap only covers a socket that does
// not honour it. See Close for why abandoning the wait is safe.
const indexWorkerShutdownWait = 5 * time.Second

// indexMaintConnectTimeout bounds establishing the pass' dedicated
// maintenance connection. Index maintenance is best-effort, so a database
// that is slow to accept a new session skips the pass rather than parking the
// worker on a dial.
const indexMaintConnectTimeout = 10 * time.Second

// indexMaintCloseTimeout bounds tearing the maintenance connection down. The
// teardown runs on a context derived with context.WithoutCancel so a
// cancelled pass still gets its Terminate attempt; if even that times out pgx
// closes the socket regardless, and the server-side backend releases the
// advisory lock when it notices.
const indexMaintCloseTimeout = 2 * time.Second

// execQuerier is the sliver of pgx that the index-maintenance helpers need.
// *pgxpool.Pool, *pgxpool.Conn and *pgx.Conn all satisfy it, which is what
// lets the whole pass run on ONE dedicated non-pooled session (required: the
// advisory lock below is session-level) while the ad-hoc probes used by tests
// keep using the pool.
type execQuerier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// CreateIndex registers searchFields for SEARCH and queues the B-tree
// expression indexes backing them, mirroring what the SQLite driver does.
//
// TWO indexes are built per field, because the query compiler emits two
// different expressions for the same JSONB leaf:
//
//   - text:    ((data->>'f'))                       — dialect.Eq / Matches /
//     Contains / In and the ORDER BY tiebreaker.
//   - numeric: ((CASE WHEN data->>'f' ~ '^-?[0-9]+(\.[0-9]+)?$'
//     THEN (data->>'f')::numeric END))
//     WHERE that CASE IS NOT NULL                  — dialect.typedCompare's
//     numeric branch, i.e. the range and equality PREDICATES.
//
// What the numeric index buys, stated narrowly: range and equality predicates
// on a numeric field — the housekeeper's `date_epoch < $1` sweep and any
// filter the query compiler routes through typedCompare's numeric branch. It
// does NOT serve renderOrderBy. renderOrderBy emits the same numericExpr as
// its primary sort key, but the ORDER BY is a two-key
// "numericExpr <dir> NULLS LAST, pathText <dir> NULLS LAST" tuple, and no
// single-key btree can satisfy a two-key sort: the planner still adds a Sort
// node (TestNumericExpressionIndexServesRangePredicate pins exactly
// that plan shape). Any
// claim that a paged, sorted listing is index-only is wrong; only the
// predicates benefit.
//
// Both come from the same helpers the compiler uses (pathText / numericExpr),
// which is the whole point: Postgres matches an expression index only when the
// query's expression is the same one, so a hand-written variant here would
// build an index nothing can use. Before this, only the text index existed and
// `date_epoch < $1` (numeric) never touched an index at all. The CASE guard in
// numericExpr is also what lets the numeric index build at all — a bare
// ::numeric cast makes CREATE INDEX fail on the first row holding
// non-numeric text.
//
// A GIN index on the whole `data` column already exists (created lazily by
// ensureCollection), but it serves neither ORDER BY nor range scans on a
// single key: `notificationlog` is sorted and swept by date_epoch, and without
// these indexes a 30-day log means a sequential scan per page.
//
// NOTHING in this function blocks on an index build. The two synchronous parts
// are the in-memory SEARCH registration and ensureCollection (idempotent
// CREATE TABLE / GIN DDL, a no-op after the first call); the builds themselves
// are queued for the driver's single background worker. That is deliberate and
// caller-independent: CREATE INDEX CONCURRENTLY waits out every transaction
// older than the build, so an inline build on the boot path stalls startup for
// as long as the busiest open transaction lives. plugins.Build already
// dispatches its pass to a goroutine, but plugin PostInit hooks call
// CreateIndex directly on the boot critical path (heartbeat indexes its
// `token` field), and those callers must not have to know.
//
// Index creation is best-effort and NEVER surfaced as an error: it is a
// performance concern, and boot must not fail because a CONCURRENTLY build
// lost a race or the database is read-only. Failures are logged at Warn by the
// worker with the index name (which callers cannot know); only indexes that
// were actually created log an Info line. Errors from ensureCollection DO
// propagate — that one is a real schema problem.
//
// A field whose name is not a plain identifier path is skipped rather than
// interpolated: the expression is spliced into DDL, and the SEARCH
// registration is the part that actually matters for correctness.
func (d *Driver) CreateIndex(ctx context.Context, collection string, fields []string) error {
	// Register the SEARCH field list FIRST, before any DDL. It is a plain
	// in-memory map write, whereas ensureCollection below can fail outright.
	// Registering up front means a slow or failed index build costs
	// performance only: SEARCH is correctly scoped from the moment
	// CreateIndex is entered.
	d.mu.Lock()
	d.searchFields[collection] = append([]string(nil), fields...)
	d.mu.Unlock()

	table, err := d.ensureCollection(ctx, collection)
	if err != nil {
		return err
	}

	d.enqueueIndexJob(indexJob{
		collection: collection,
		table:      table,
		fields:     append([]string(nil), fields...),
	})
	return nil
}

// enqueueIndexJob appends one job to the background worker's queue and nudges
// it. Never blocks: the queue is an unbounded slice under a mutex and the
// wake-up channel has capacity 1, so a nudge that finds one already pending is
// dropped (the worker re-reads the whole queue anyway).
//
// The job's context is deliberately NOT the caller's: an index build outlives
// the boot context that requested it. Cancellation comes from Close.
//
// A collection with a Drop in flight is skipped: see idxDropping. Skipping
// costs only the expression indexes (the SEARCH registration in CreateIndex
// already happened, and it is the half that matters for correctness); the next
// boot re-queues them.
func (d *Driver) enqueueIndexJob(job indexJob) {
	if len(job.fields) == 0 || d.idxSignal == nil {
		return
	}
	d.idxMu.Lock()
	if d.idxClosing {
		d.idxMu.Unlock()
		return
	}
	if d.idxDropping[job.collection] > 0 {
		d.idxMu.Unlock()
		d.log().Debug("postgres: index builds skipped; the collection is being dropped",
			"collection", job.collection)
		return
	}
	d.idxQueue = append(d.idxQueue, job)
	d.idxMu.Unlock()
	select {
	case d.idxSignal <- struct{}{}:
	default:
	}
}

// indexWorker is the driver's single index-maintenance goroutine, started by
// New and stopped by Close. One goroutine means N collections produce N
// sequential builds rather than N concurrent ones fighting over the pool, and
// it is also what makes the cross-replica advisory lock cheap: it is taken once
// per pass, not once per index.
func (d *Driver) indexWorker() {
	defer close(d.idxDone)
	for {
		// batch, its context and its cancel come out of ONE idxMu window:
		// see takeIndexBatch for why that matters.
		batch, passCtx, cancelPass := d.takeIndexBatch()
		if len(batch) == 0 {
			select {
			case <-d.idxCtx.Done():
				return
			case <-d.idxSignal:
				continue
			}
		}
		d.runIndexPass(passCtx, batch)
		cancelPass()
		d.idxMu.Lock()
		d.idxPassCancel = nil
		d.idxBusy = false
		d.idxMu.Unlock()
		if d.idxCtx.Err() != nil {
			return
		}
	}
}

// takeIndexBatch drains the queue in one go so a burst of CreateIndex calls
// (boot registers every collection at once) becomes a single pass under a
// single advisory lock. It returns the batch together with the context that
// governs the pass and that context's cancel func.
//
// Building the pass context HERE, inside the same idxMu window that sets
// idxBusy, is load-bearing. The worker used to set idxBusy in this window and
// publish idxPassCancel in a later one, so `idxBusy && idxPassCancel == nil`
// was an observable state — and abortIndexWork, which reads both under the
// mutex, treats a nil cancel as "nothing to stop" and falls straight through
// to flushIndexBuilds. A Drop landing in that window therefore waited out the
// FULL uncancelled pass, which for a CREATE INDEX CONCURRENTLY parked behind a
// long-lived writer is unbounded. Publishing both atomically makes that state
// unrepresentable.
//
// The returned cancel is never nil when the batch is non-empty; the caller
// owns it and must call it once the pass has returned.
func (d *Driver) takeIndexBatch() ([]indexJob, context.Context, context.CancelFunc) {
	d.idxMu.Lock()
	defer d.idxMu.Unlock()
	if len(d.idxQueue) == 0 {
		d.idxBusy = false
		d.idxPassCancel = nil
		return nil, nil, nil
	}
	batch := d.idxQueue
	d.idxQueue = nil
	d.idxBusy = true
	passCtx, cancelPass := context.WithCancel(d.idxCtx)
	d.idxPassCancel = cancelPass
	return batch, passCtx, cancelPass
}

// flushIndexBuilds blocks until the worker has no queued and no in-flight
// work, or ctx expires. Used by the container tests, which assert on the
// catalog right after CreateIndex returns.
func (d *Driver) flushIndexBuilds(ctx context.Context) error {
	if d.idxDone == nil {
		return nil
	}
	for {
		d.idxMu.Lock()
		idle := len(d.idxQueue) == 0 && !d.idxBusy
		d.idxMu.Unlock()
		if idle {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-d.idxDone:
			return nil
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// abortIndexWork discards queued index work for collection and stops any pass
// already running, then waits for the worker to go idle.
//
// Every caller that takes an ACCESS EXCLUSIVE lock on a collection's table
// MUST do this first. DROP TABLE and a CREATE INDEX CONCURRENTLY on the same
// relation deadlock outright: the DDL blocks on the build's
// ShareUpdateExclusive lock while the build's "wait for lockers" phase blocks
// on the DDL's transaction, and Postgres resolves it by killing one of them
// (observed as "drop record: deadlock detected"). Waiting is the only safe
// ordering, and it is cheap in practice because Drop is an administrative
// operation.
//
// A running pass is cancelled wholesale, not filtered: a pass carries a batch
// of collections and the one being dropped may still be ahead of the cursor.
// Whatever it had left is simply lost — index maintenance is best-effort and
// the next boot re-queues it — and any half-built index it leaves behind is
// exactly the invalid leftover ensureExprIndex already knows how to rebuild.
//
// busy and cancel are read in ONE idxMu window and takeIndexBatch publishes
// them in one, so "busy with nothing to cancel" cannot happen: whenever this
// function decides to wait, it has already cancelled what it is waiting for.
func (d *Driver) abortIndexWork(ctx context.Context, collection string) {
	d.idxMu.Lock()
	kept := d.idxQueue[:0]
	for _, j := range d.idxQueue {
		if j.collection != collection {
			kept = append(kept, j)
		}
	}
	d.idxQueue = kept
	cancel := d.idxPassCancel
	busy := d.idxBusy
	d.idxMu.Unlock()
	if !busy {
		return
	}
	if cancel != nil {
		cancel()
	}
	// Best-effort: a ctx that is already done leaves the caller to race the
	// build exactly as it did before, which Postgres reports rather than hangs.
	_ = d.flushIndexBuilds(ctx)
}

// beginDropGuard marks collection as being dropped, so enqueueIndexJob
// refuses new index work for it until the matching endDropGuard.
func (d *Driver) beginDropGuard(collection string) {
	d.idxMu.Lock()
	defer d.idxMu.Unlock()
	if d.idxDropping == nil {
		d.idxDropping = map[string]int{}
	}
	d.idxDropping[collection]++
}

// endDropGuard releases one beginDropGuard. The map entry is deleted at zero
// so a long-lived driver does not accumulate one entry per dropped
// collection.
func (d *Driver) endDropGuard(collection string) {
	d.idxMu.Lock()
	defer d.idxMu.Unlock()
	if d.idxDropping[collection] <= 1 {
		delete(d.idxDropping, collection)
		return
	}
	d.idxDropping[collection]--
}

// runIndexPass builds every queued index on ONE dedicated NON-POOLED
// connection, serialised across replicas by a session-level advisory lock.
//
// Why not a pooled connection, which is what this used to acquire:
//
//   - Starvation. A pass pins its connection for its whole duration — every
//     CONCURRENTLY build in the batch, each of which waits out every
//     transaction older than itself. With `database.pool_max_size: 1` (or 2,
//     both of which real deployments use) that is the entire pool, so the
//     server serves requests with one connection less, or none, for minutes.
//   - A leaked advisory lock. The lock is session-level because CONCURRENTLY
//     cannot run in a transaction, so it has to be released explicitly — and
//     if that unlock failed or timed out, a HEALTHY connection went back to
//     the pool still holding it. Nothing ever unlocks it after that, and every
//     later pass in the process (and, until the process dies, on every
//     replica) sees pg_try_advisory_lock return false and skips. Index
//     maintenance would be silently dead for the rest of the deployment's
//     uptime.
//
// A dedicated session removes both: nothing else waits on it, and closing it
// at the end of the pass is what releases the lock — session death is the
// release mechanism, so there is no unlock round-trip that can fail and no
// state that outlives the pass. Cancellation (Close, or Drop via
// abortIndexWork) tears the same session down, which is what makes the
// shutdown wait honest; see Close.
//
// Why the lock. CREATE INDEX CONCURRENTLY publishes its index with
// indisvalid = false for the entire duration of the build, and only flips it
// true at the end. Without coordination, replica B booting while replica A is
// mid-build sees "invalid" and concludes the index is an interrupted leftover:
// its DROP INDEX CONCURRENTLY then blocks until A's build finishes and
// promptly drops the freshly valid index. One holder at a time removes the
// whole class of problem, and the loser simply skips: the winner is building
// exactly the same set of indexes, so there is nothing to retry. The lock is
// session-level (not transaction-level) because CONCURRENTLY cannot run inside
// a transaction; closing the connection below is what releases it.
func (d *Driver) runIndexPass(ctx context.Context, jobs []indexJob) {
	lg := d.log()
	conn, err := d.maintenanceConn(ctx)
	if err != nil {
		if ctx.Err() == nil {
			lg.Warn("postgres: index pass skipped; no maintenance connection", "err", err)
		}
		return
	}
	defer func() {
		// Closing the session releases the advisory lock, so this is both the
		// teardown and the unlock. WithoutCancel so a cancelled pass still
		// gets its polite Terminate; pgx closes the socket regardless if the
		// timeout expires, and the backend then drops the lock on its own.
		cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), indexMaintCloseTimeout)
		defer cancel()
		if cerr := conn.Close(cctx); cerr != nil {
			lg.Debug("postgres: closing the index-maintenance connection failed",
				"err", cerr)
		}
	}()

	var locked bool
	if err := conn.QueryRow(ctx,
		"SELECT pg_try_advisory_lock(hashtext($1))", indexMaintLockKey).Scan(&locked); err != nil {
		if ctx.Err() == nil {
			lg.Warn("postgres: index pass skipped; advisory lock probe failed",
				"lock", indexMaintLockKey, "err", err)
		}
		return
	}
	if !locked {
		lg.Info("postgres: another server is doing index maintenance; skipping this pass",
			"lock", indexMaintLockKey)
		return
	}

	for _, job := range jobs {
		for _, f := range job.fields {
			if sanitizeFieldIdent(f) == "" {
				continue
			}
			for _, k := range indexKinds {
				if ctx.Err() != nil {
					return
				}
				name := indexName(job.table, f, k.kind)
				created, err := d.ensureExprIndex(ctx, conn, name, job.table, f, k)
				switch {
				case err != nil:
					if ctx.Err() != nil {
						return
					}
					lg.Warn("postgres: expression index not created; queries on this field stay sequential",
						"index", name, "collection", job.collection, "field", f, "err", err)
				case created:
					lg.Info("postgres: created expression index",
						"index", name, "collection", job.collection, "field", f)
				}
			}
		}
	}
}

// maintenanceConn opens the pass' own connection, outside the pool, from the
// pool's parsed connection config — same DSN, same TLS, same runtime params,
// so an operator does not have to configure the maintenance path separately.
//
// application_name gets a " index-maint" suffix: a CONCURRENTLY build can sit
// in pg_stat_activity for a long time, and a DBA looking at it should be able
// to tell it apart from request traffic without reading the query text.
func (d *Driver) maintenanceConn(ctx context.Context) (*pgx.Conn, error) {
	if d.pool == nil {
		return nil, errors.New("postgres: driver has no pool")
	}
	cfg := d.pool.Config().ConnConfig.Copy()
	if cfg.RuntimeParams == nil {
		cfg.RuntimeParams = map[string]string{}
	}
	name := cfg.RuntimeParams["application_name"]
	if name == "" {
		name = "snooze"
	}
	cfg.RuntimeParams["application_name"] = name + " index-maint"

	dialCtx, cancel := context.WithTimeout(ctx, indexMaintConnectTimeout)
	defer cancel()
	conn, err := pgx.ConnectConfig(dialCtx, cfg)
	if err != nil {
		return nil, fmt.Errorf("postgres: index-maintenance connect: %w", err)
	}
	return conn, nil
}

// ensureExprIndex creates one expression index unless an identically named,
// valid index already exists. It reports whether the CREATE actually ran.
//
// CREATE INDEX CONCURRENTLY is what keeps index maintenance off the write
// path: a plain CREATE INDEX takes an ACCESS EXCLUSIVE lock, so a fresh
// deployment (or a version that adds a search field to an existing 30-day
// notificationlog) would block every write on that table until the build
// finishes. CONCURRENTLY cannot run inside a transaction — hence the plain
// Exec on an auto-commit session, and never a tx.
//
// The catalog probe does two jobs. It keeps the Info log honest (only real
// creations are logged), and it defuses the INVALID-index hazard: an
// interrupted CONCURRENTLY build leaves the index in place with
// indisvalid = false — dead weight the planner ignores — and a later
// "CREATE INDEX CONCURRENTLY IF NOT EXISTS" sees the name and skips, so the
// index would stay broken forever. We drop the invalid leftover (also
// CONCURRENTLY, also outside a tx) and rebuild it.
//
// "Invalid" alone is NOT proof of an interrupted build, though: a build in
// progress looks exactly the same. runIndexPass' advisory lock excludes a
// racing peer that got as far as this function, and pg_stat_progress_create_index
// covers the rest (a build started by a peer that has since dropped the lock,
// or a DBA's manual REINDEX CONCURRENTLY) — if a build on this index or its
// table is in flight, the leftover is left alone for the next boot to judge.
func (d *Driver) ensureExprIndex(ctx context.Context, q execQuerier, name, table, field string, k indexKind) (bool, error) {
	exists, valid, err := indexState(ctx, q, name)
	if err != nil {
		return false, err
	}
	switch {
	case exists && valid:
		return false, nil
	case exists && !valid:
		building, err := indexBuildInProgress(ctx, q, name, table)
		if err != nil {
			// Conservative: an unreadable progress view must not authorise
			// dropping what may be a live build. The next boot retries.
			return false, fmt.Errorf("index %s is invalid and its build state is unknown: %w", name, err)
		}
		if building {
			d.log().Info("postgres: invalid index left alone; a build is in progress",
				"index", name, "table", table)
			return false, nil
		}
		if _, err := q.Exec(ctx, "DROP INDEX CONCURRENTLY IF EXISTS "+quoteIdent(name)); err != nil {
			return false, fmt.Errorf("drop invalid index %s: %w", name, err)
		}
	}
	expr := k.expr(field)
	stmt := fmt.Sprintf("CREATE INDEX CONCURRENTLY IF NOT EXISTS %s ON %s ((%s))",
		quoteIdent(name), quoteIdent(table), expr)
	if k.partial {
		// Byte-identical expression on purpose: the planner proves
		// "expr <op> $n implies expr IS NOT NULL" by matching the expression
		// tree, so any divergence here makes the index unusable.
		stmt += fmt.Sprintf(" WHERE (%s) IS NOT NULL", expr)
	}
	if _, err := q.Exec(ctx, stmt); err != nil {
		return false, fmt.Errorf("create index %s: %w", name, err)
	}
	return true, nil
}

// indexState reports whether an index of this name exists in the search path
// and, if so, whether it is valid (indisvalid). pg_indexes cannot answer the
// second half, which is why this reads pg_index directly.
func (d *Driver) indexState(ctx context.Context, name string) (exists, valid bool, err error) {
	return indexState(ctx, d.pool, name)
}

func indexState(ctx context.Context, q execQuerier, name string) (exists, valid bool, err error) {
	row := q.QueryRow(ctx,
		"SELECT i.indisvalid FROM pg_index i "+
			"JOIN pg_class c ON c.oid = i.indexrelid "+
			"JOIN pg_namespace n ON n.oid = c.relnamespace "+
			"WHERE c.relname = $1 AND n.nspname = ANY (current_schemas(false)) LIMIT 1",
		name)
	if scanErr := row.Scan(&valid); scanErr != nil {
		if errors.Is(scanErr, pgx.ErrNoRows) {
			return false, false, nil
		}
		return false, false, fmt.Errorf("postgres: probe index %s: %w", name, scanErr)
	}
	return true, valid, nil
}

// indexBuildInProgress reports whether Postgres is currently building this
// index, or any index on its table. Both halves matter: early in a
// CONCURRENTLY build pg_stat_progress_create_index may not yet carry the
// index_relid, and a build on the same table is a peer doing the same pass
// (whose other indexes we must not disturb either).
//
// pg_stat_progress_create_index needs Postgres 12+ and only shows sessions the
// caller may inspect — which covers the case that matters here, since every
// replica connects as the same database role.
//
// quote_ident around the arguments is not cosmetic. to_regclass parses its
// argument as an SQL identifier, so it CASE-FOLDS an unquoted one: the indexes
// here are created with quoteIdent (always double-quoted, so the catalog keeps
// the case exactly as written), and a mixed-case search field like `dateEpoch`
// yields an index named idx_..._dateEpoch that to_regclass('idx_..._dateEpoch')
// resolves to NULL. Both halves of the predicate then compare against NULL,
// count(*) is 0, and this reports "no build in progress" for a peer's LIVE
// build — whose freshly valid index the caller would go on to drop. That is
// precisely the replica hazard the probe exists to prevent, so the failure
// mode is silent and maximally bad. quote_ident re-quotes when needed and is a
// no-op for the all-lowercase names, so the fix costs nothing.
func indexBuildInProgress(ctx context.Context, q execQuerier, name, table string) (bool, error) {
	var n int
	if err := q.QueryRow(ctx,
		"SELECT count(*) FROM pg_stat_progress_create_index p "+
			"WHERE p.index_relid = to_regclass(quote_ident($1))::oid "+
			"OR p.relid = to_regclass(quote_ident($2))::oid",
		name, table).Scan(&n); err != nil {
		return false, fmt.Errorf("postgres: probe create-index progress: %w", err)
	}
	return n > 0, nil
}

// sanitizeFieldIdent renders a search field as a bare SQL identifier fragment
// for the index NAME (dots become "__"). It returns "" for a field carrying
// anything other than [A-Za-z0-9_.], which is the signal to skip the index.
func sanitizeFieldIdent(field string) string {
	if field == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range field {
		switch {
		case r == '.':
			b.WriteString("__")
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_':
			b.WriteRune(r)
		default:
			return ""
		}
	}
	return b.String()
}

// indexName builds the index identifier for one (table, field, kind) triple.
// kind is "" for the text index and "num" for the guarded-numeric one; the
// text index keeps the plain unsuffixed name `idx_<table>_<field>`.
//
// Two distinct fields must never produce the same name, because two colliding
// names mean the second CREATE INDEX ... IF NOT EXISTS is a silent no-op: the
// field looks indexed and is not. There are two ways to collide, and both end
// in the same fix — an 8-hex-digit sha1 of the untruncated "table|field|kind"
// appended to the name (sha1 as a short digest, not for any security
// property):
//
//  1. Length. Postgres silently truncates identifiers past
//     NAMEDATALEN-1 = 63 bytes, so we truncate ourselves, and a plain cut
//     collides whenever two long field paths share a long prefix
//     ("a.very.long.path.alpha" and "a.very.long.path.beta").
//  2. Sanitisation. sanitizeFieldIdent is not injective: it maps "." to "__",
//     so the field "a.b" and the (legal) field "a__b" both render "a__b".
//     Whenever sanitisation changed the field at all the name therefore
//     carries the digest, short or not.
func indexName(table, field, kind string) string {
	ident := sanitizeFieldIdent(field)
	name := "idx_" + table + "_" + ident
	if kind != "" {
		name += "_" + kind
	}
	if ident == field && len(name) <= maxIdentLen {
		return name
	}
	sum := sha1.Sum([]byte(table + "|" + field + "|" + kind)) //nolint:gosec // digest, not a MAC
	suffix := "_" + hex.EncodeToString(sum[:])[:8]
	if len(name)+len(suffix) <= maxIdentLen {
		return name + suffix
	}
	return name[:maxIdentLen-len(suffix)] + suffix
}

// maxIdentLen is Postgres' NAMEDATALEN-1 identifier limit.
const maxIdentLen = 63

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
	// DROP TABLE takes ACCESS EXCLUSIVE, which deadlocks against an in-flight
	// CREATE INDEX CONCURRENTLY on the same relation. See abortIndexWork.
	//
	// The drop guard goes up BEFORE the abort and comes down when this
	// function returns: abortIndexWork clears the queue and waits for the
	// worker to go idle, but nothing stopped a concurrent CreateIndex on the
	// same collection from enqueuing (and the worker from picking up) a new
	// build in the gap between that wait returning and the DROP below taking
	// its lock — the deadlock the abort exists to prevent, reintroduced by a
	// racing caller. With the guard in place enqueueIndexJob drops such a job
	// on the floor instead.
	d.beginDropGuard(collection)
	defer d.endDropGuard(collection)
	d.abortIndexWork(ctx, collection)
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
