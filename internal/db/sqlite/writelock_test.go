// Regression tests for the write-transaction locking mode.
//
// SQLite in WAL mode does NOT retry a transaction that opened DEFERRED,
// took a read snapshot, and then tries to upgrade to a writer while another
// connection has moved the WAL forward: it fails immediately with
// SQLITE_BUSY_SNAPSHOT ("database is locked"), and `busy_timeout` does not
// apply to that case. Every write path in this driver reads before it writes
// (primary-key lookup, read-modify-write patch), so a deferred BEGIN made
// concurrent writers fail spuriously on the ingest path.
//
// The driver therefore opens write transactions with BEGIN IMMEDIATE, which
// takes the write lock up front and *is* covered by busy_timeout.

package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/condition"
	dbpkg "github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// TestDeferredTxUpgradeIsBusy documents the failure mode the driver has to
// avoid: a DEFERRED transaction that reads first and writes second loses to
// any writer that committed in between, immediately and without retry.
//
// If this test ever starts failing because SQLite learned to retry the
// upgrade, the BEGIN IMMEDIATE mechanism below becomes belt-and-braces
// rather than load-bearing — it stays correct either way.
func TestDeferredTxUpgradeIsBusy(t *testing.T) {
	d := newTestDriver(t)
	ctx := snoozetypes.WithPlatformScope(context.Background())
	const coll = "record"
	require.NoError(t, d.ensure(ctx, coll))
	tbl, err := tableName(coll)
	require.NoError(t, err)

	// A second pool over the same file with the pre-fix locking mode, so the
	// transaction below is genuinely DEFERRED. (On d.db every BeginTx is
	// IMMEDIATE now, which is the whole point of the fix.)
	deferredDSN := strings.Replace(buildDSN(d.cfg), "_txlock=immediate", "_txlock=deferred", 1)
	require.Contains(t, deferredDSN, "_txlock=deferred")
	legacy, err := sql.Open("sqlite", deferredDSN)
	require.NoError(t, err)
	defer func() { _ = legacy.Close() }()

	// The SELECT pins a read snapshot; the tx is not yet a writer.
	tx, err := legacy.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	var n int
	require.NoError(t, tx.QueryRowContext(ctx,
		fmt.Sprintf("SELECT COUNT(*) FROM %s", quoteIdent(tbl))).Scan(&n)) //nolint:gosec

	// Another connection commits a write, moving the WAL past our snapshot.
	// This also proves an open reader does not block an IMMEDIATE writer:
	// WAL keeps readers and the single writer concurrent.
	_, err = d.Write(ctx, coll, []dbpkg.Document{{"hash": "other"}},
		dbpkg.WriteOptions{Primary: []string{"hash"}})
	require.NoError(t, err)

	// Upgrading the deferred tx to a writer now fails outright.
	_, err = tx.ExecContext(ctx,
		fmt.Sprintf("INSERT INTO %s (uid, data, seq) VALUES (?, ?, 99)", quoteIdent(tbl)), //nolint:gosec
		"deferred-uid", `{"uid":"deferred-uid","hash":"deferred"}`)
	require.ErrorContains(t, err, "database is locked",
		"a deferred read-then-write tx should fail with SQLITE_BUSY_SNAPSHOT")
}

// TestWriteTxHoldsWriteLockFromBegin is the same scenario driven through the
// driver's own write-transaction helper: because it opens IMMEDIATE, it owns
// the write lock before it reads, a concurrent Write blocks at its BEGIN
// (honouring busy_timeout) instead of failing, and both succeed.
func TestWriteTxHoldsWriteLockFromBegin(t *testing.T) {
	d := newTestDriver(t)
	ctx := snoozetypes.WithPlatformScope(context.Background())
	const coll = "record"
	require.NoError(t, d.ensure(ctx, coll))
	tbl, err := tableName(coll)
	require.NoError(t, err)

	tx, err := d.beginWrite(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	var n int
	require.NoError(t, tx.QueryRowContext(ctx,
		fmt.Sprintf("SELECT COUNT(*) FROM %s", quoteIdent(tbl))).Scan(&n)) //nolint:gosec

	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		close(started)
		_, werr := d.Write(ctx, coll, []dbpkg.Document{{"hash": "other"}},
			dbpkg.WriteOptions{Primary: []string{"hash"}})
		done <- werr
	}()
	<-started
	// Give the concurrent writer time to reach (and block on) its BEGIN.
	time.Sleep(200 * time.Millisecond)

	_, err = tx.ExecContext(ctx,
		fmt.Sprintf("INSERT INTO %s (uid, data, seq) VALUES (?, ?, 99)", quoteIdent(tbl)), //nolint:gosec
		"held-uid", `{"uid":"held-uid","hash":"held"}`)
	require.NoError(t, err, "a write tx must not lose its write lock to a concurrent writer")
	require.NoError(t, tx.Commit())

	select {
	case werr := <-done:
		require.NoError(t, werr, "the blocked writer must proceed once the lock is released")
	case <-time.After(10 * time.Second):
		t.Fatal("concurrent Write never completed")
	}
}

// TestConcurrentWritesNoBusy mirrors the ingest path that flaked: several
// goroutines upserting by primary key and patching fields on the same
// collection at once. Every one of these does SELECT-then-write inside one
// transaction, so under a deferred BEGIN a fraction of them died with
// "database is locked".
func TestConcurrentWritesNoBusy(t *testing.T) {
	d := newTestDriver(t)
	ctx := snoozetypes.WithPlatformScope(context.Background())
	const coll = "record"
	require.NoError(t, d.ensure(ctx, coll))

	const workers, perWorker = 8, 15
	errs := make(chan error, workers*perWorker*2)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				hash := fmt.Sprintf("h-%d-%d", w, i)
				if _, err := d.Write(ctx, coll,
					[]dbpkg.Document{{"hash": hash, "count": 1}},
					dbpkg.WriteOptions{Primary: []string{"hash"}}); err != nil {
					errs <- fmt.Errorf("write %s: %w", hash, err)
					continue
				}
				if _, err := d.SetFields(ctx, coll,
					dbpkg.Document{"state": "ack"},
					condition.Equals("hash", hash)); err != nil {
					errs <- fmt.Errorf("setfields %s: %w", hash, err)
				}
			}
		}(w)
	}
	wg.Wait()
	close(errs)
	var collected []error
	for err := range errs {
		collected = append(collected, err)
	}
	require.Emptyf(t, collected, "concurrent writers must not fail: %v", collected)
}
