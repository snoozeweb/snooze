package mq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PGConfig configures the Postgres-backed Bus. Either DSN or Pool must be
// set; Pool is preferred when sharing the pool with the db driver.
type PGConfig struct {
	DSN     string
	Pool    *pgxpool.Pool
	PoolMin int
	PoolMax int
	// ApplicationName is set on every pooled connection so DB-side
	// dashboards can attribute load.
	ApplicationName string
	// VisibilityTimeout is the duration after which a locked-but-unacked
	// message becomes claimable again. Defaults to 5 minutes.
	VisibilityTimeout time.Duration
	// ConsumerID identifies this consumer in the locked_by column. Defaults
	// to a random uuid.
	ConsumerID string
}

// pgBus is the Postgres LISTEN/NOTIFY + SELECT...FOR UPDATE SKIP LOCKED
// implementation of Bus.
type pgBus struct {
	cfg      PGConfig
	pool     *pgxpool.Pool
	ownsPool bool

	closeOnce sync.Once
	closeCh   chan struct{}
	wg        sync.WaitGroup
}

// NewPG builds a Postgres-backed Bus. When cfg.Pool is nil, a fresh pool is
// dialled from cfg.DSN and owned by the returned Bus (closed on Close).
func NewPG(ctx context.Context, cfg PGConfig) (Bus, error) {
	if cfg.VisibilityTimeout <= 0 {
		cfg.VisibilityTimeout = 5 * time.Minute
	}
	if cfg.ConsumerID == "" {
		cfg.ConsumerID = uuid.NewString()
	}
	pool := cfg.Pool
	owns := false
	if pool == nil {
		if cfg.DSN == "" {
			return nil, errors.New("mq pg: either Pool or DSN must be set")
		}
		pcfg, err := pgxpool.ParseConfig(cfg.DSN)
		if err != nil {
			return nil, fmt.Errorf("mq pg: parse dsn: %w", err)
		}
		if cfg.PoolMin > 0 {
			pcfg.MinConns = int32(cfg.PoolMin) //nolint:gosec
		}
		if cfg.PoolMax > 0 {
			pcfg.MaxConns = int32(cfg.PoolMax) //nolint:gosec
		}
		if cfg.ApplicationName != "" {
			if pcfg.ConnConfig.RuntimeParams == nil {
				pcfg.ConnConfig.RuntimeParams = map[string]string{}
			}
			pcfg.ConnConfig.RuntimeParams["application_name"] = cfg.ApplicationName
		}
		pool, err = pgxpool.NewWithConfig(ctx, pcfg)
		if err != nil {
			return nil, fmt.Errorf("mq pg: connect: %w", err)
		}
		owns = true
	}
	b := &pgBus{
		cfg:      cfg,
		pool:     pool,
		ownsPool: owns,
		closeCh:  make(chan struct{}),
	}
	if err := b.ensureSchema(ctx); err != nil {
		if owns {
			pool.Close()
		}
		return nil, err
	}
	return b, nil
}

// ensureSchema creates the mq_messages table and supporting indexes if absent.
func (b *pgBus) ensureSchema(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS mq_messages (
			id          TEXT PRIMARY KEY,
			queue       TEXT NOT NULL,
			payload     JSONB NOT NULL,
			headers     JSONB,
			locked_at   TIMESTAMPTZ,
			locked_by   TEXT,
			ack_at      TIMESTAMPTZ,
			created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_mq_messages_queue_ack ON mq_messages (queue, ack_at)`,
		`CREATE INDEX IF NOT EXISTS idx_mq_messages_created_at ON mq_messages (created_at)`,
	}
	tx, err := b.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("mq pg: begin schema tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	for _, s := range stmts {
		if _, err := tx.Exec(ctx, s); err != nil {
			return fmt.Errorf("mq pg: ddl: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("mq pg: commit schema: %w", err)
	}
	return nil
}

// channelFor returns the LISTEN channel name for a given queue. Postgres
// identifiers cap at 63 chars, so excessive queue names are truncated.
func channelFor(queue string) string {
	c := "mq_" + sanitizeQueueIdent(queue)
	if len(c) > 63 {
		c = c[:63]
	}
	return c
}

// sanitizeQueueIdent replaces every character that isn't ASCII alphanumeric
// or underscore with an underscore. The queue name itself is stored verbatim
// in the table; only the LISTEN channel is sanitised.
func sanitizeQueueIdent(s string) string {
	var sb strings.Builder
	sb.Grow(len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
			sb.WriteRune(r)
		default:
			sb.WriteByte('_')
		}
	}
	return sb.String()
}

// Publish inserts a row and emits pg_notify on the queue's channel.
func (b *pgBus) Publish(ctx context.Context, queue string, payload any) error {
	enc, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("mq pg: marshal: %w", err)
	}
	id := uuid.NewString()
	tx, err := b.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("mq pg: begin publish tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx,
		`INSERT INTO mq_messages (id, queue, payload) VALUES ($1, $2, $3::jsonb)`,
		id, queue, string(enc),
	); err != nil {
		return fmt.Errorf("mq pg: insert: %w", err)
	}
	if _, err := tx.Exec(ctx, `SELECT pg_notify($1, $2)`, channelFor(queue), id); err != nil {
		return fmt.Errorf("mq pg: notify: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("mq pg: commit publish: %w", err)
	}
	return nil
}

// Subscribe launches a consumer loop per Concurrency that listens for
// notifications and polls the table on each notify or BatchTimer.
func (b *pgBus) Subscribe(ctx context.Context, queue string, opts SubscribeOpts, h Handler) error {
	if h == nil {
		return errors.New("mq pg: nil handler")
	}
	opts = defaults(opts)
	for i := 0; i < opts.Concurrency; i++ {
		b.wg.Add(1)
		go b.consumerLoop(ctx, queue, opts, h)
	}
	return nil
}

// consumerLoop owns one dedicated connection for LISTEN and polls the
// claim query on each notification or batch timer tick.
func (b *pgBus) consumerLoop(parentCtx context.Context, queue string, opts SubscribeOpts, h Handler) {
	defer b.wg.Done()
	backoff := 200 * time.Millisecond
	for {
		select {
		case <-parentCtx.Done():
			return
		case <-b.closeCh:
			return
		default:
		}

		if err := b.runConsumer(parentCtx, queue, opts, h); err != nil {
			select {
			case <-parentCtx.Done():
				return
			case <-b.closeCh:
				return
			case <-time.After(backoff):
			}
			if backoff < 5*time.Second {
				backoff *= 2
			}
			continue
		}
		backoff = 200 * time.Millisecond
	}
}

// runConsumer holds the listen connection until it drops, claiming and
// dispatching batches.
func (b *pgBus) runConsumer(parentCtx context.Context, queue string, opts SubscribeOpts, h Handler) error {
	conn, err := b.pool.Acquire(parentCtx)
	if err != nil {
		return fmt.Errorf("mq pg: acquire: %w", err)
	}
	hijacked := conn.Hijack()
	defer hijacked.Close(context.Background()) //nolint:errcheck

	if _, err := hijacked.Exec(parentCtx, "LISTEN "+pgIdent(channelFor(queue))); err != nil {
		return fmt.Errorf("mq pg: LISTEN: %w", err)
	}

	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()
	go func() {
		select {
		case <-b.closeCh:
			cancel()
		case <-ctx.Done():
		}
	}()

	// Initial pass: claim any rows already waiting before LISTEN was
	// registered.
	if err := b.claimAndProcess(ctx, queue, opts, h); err != nil {
		return err
	}

	for {
		waitCtx, waitCancel := context.WithTimeout(ctx, opts.BatchTimer)
		_, err := hijacked.WaitForNotification(waitCtx)
		waitCancel()
		if ctx.Err() != nil {
			return nil
		}
		if err != nil && !errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("mq pg: wait: %w", err)
		}
		if err := b.claimAndProcess(ctx, queue, opts, h); err != nil {
			return err
		}
	}
}

// claimAndProcess pulls one batch with SELECT...FOR UPDATE SKIP LOCKED,
// marks rows locked_by us, runs the handler, and acks on success.
func (b *pgBus) claimAndProcess(ctx context.Context, queue string, opts SubscribeOpts, h Handler) error {
	tx, err := b.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("mq pg: begin claim tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	rows, err := tx.Query(ctx, `
		SELECT id, payload, headers, created_at
		FROM mq_messages
		WHERE queue = $1
		  AND ack_at IS NULL
		  AND (locked_at IS NULL OR locked_at < now() - $3::interval)
		ORDER BY created_at
		FOR UPDATE SKIP LOCKED
		LIMIT $2
	`, queue, opts.BatchSize, formatInterval(b.cfg.VisibilityTimeout))
	if err != nil {
		return fmt.Errorf("mq pg: claim: %w", err)
	}
	type rowData struct {
		id        string
		payload   []byte
		headers   []byte
		createdAt time.Time
	}
	var batch []rowData
	for rows.Next() {
		var r rowData
		if err := rows.Scan(&r.id, &r.payload, &r.headers, &r.createdAt); err != nil {
			rows.Close()
			return fmt.Errorf("mq pg: scan: %w", err)
		}
		batch = append(batch, r)
	}
	rows.Close()
	if rerr := rows.Err(); rerr != nil {
		return fmt.Errorf("mq pg: claim rows: %w", rerr)
	}
	if len(batch) == 0 {
		// Nothing to do; commit empty tx to release any internal state.
		return tx.Commit(ctx)
	}
	ids := make([]string, 0, len(batch))
	for _, r := range batch {
		ids = append(ids, r.id)
	}
	if _, err := tx.Exec(ctx,
		`UPDATE mq_messages SET locked_at = now(), locked_by = $2 WHERE id = ANY($1)`,
		ids, b.cfg.ConsumerID,
	); err != nil {
		return fmt.Errorf("mq pg: lock: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("mq pg: commit claim: %w", err)
	}

	// Process outside the claim tx so a slow handler doesn't hold the
	// row-level lock.
	for _, r := range batch {
		headers := decodeHeaders(r.headers)
		msg := Message{
			ID:        r.id,
			Queue:     queue,
			Payload:   r.payload,
			Headers:   headers,
			Timestamp: r.createdAt,
		}
		if err := h(ctx, msg); err != nil {
			// Leave row visible for the next retry — locked_at expires
			// after VisibilityTimeout.
			continue
		}
		if _, err := b.pool.Exec(ctx,
			`UPDATE mq_messages SET ack_at = now() WHERE id = $1`, r.id,
		); err != nil {
			// Best-effort ack; the row will be retried on the next
			// VisibilityTimeout window.
			continue
		}
	}
	return nil
}

// Close shuts down all consumer loops and (if owned) the underlying pool.
// Idempotent.
func (b *pgBus) Close() error {
	b.closeOnce.Do(func() {
		close(b.closeCh)
	})
	b.wg.Wait()
	if b.ownsPool && b.pool != nil {
		b.pool.Close()
		b.pool = nil
	}
	return nil
}

// pgIdent quotes a SQL identifier safely. Our channel names are already
// sanitised, but quoting protects against accidental keyword collisions.
func pgIdent(ident string) string {
	return `"` + strings.ReplaceAll(ident, `"`, `""`) + `"`
}

// decodeHeaders unmarshals the jsonb headers column.
func decodeHeaders(raw []byte) map[string]string {
	if len(raw) == 0 {
		return nil
	}
	var out map[string]string
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return out
}

// formatInterval renders a Go Duration as a Postgres interval literal in
// microseconds (lossless within the ms-granularity range we care about).
func formatInterval(d time.Duration) string {
	if d <= 0 {
		d = 5 * time.Minute
	}
	return fmt.Sprintf("%d microseconds", d.Microseconds())
}
