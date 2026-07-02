package sqlite

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	dbpkg "github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

func TestSourceActivityEmptyCollection(t *testing.T) {
	t.Parallel()
	d := newTestDriver(t)
	ctx := snoozetypes.WithTenant(context.Background(), "default")
	rows, err := d.SourceActivity(ctx, 0)
	require.NoError(t, err)
	require.Empty(t, rows)
}

// TestSourceActivityNakedContextFailsClosed pins the [H3] invariant: `record`
// is tenant-scoped, so a context with neither a tenant nor platform scope must
// fail closed with ErrNoTenant — even before the table exists (tenant scope is
// resolved before the existence check).
func TestSourceActivityNakedContextFailsClosed(t *testing.T) {
	t.Parallel()
	d := newTestDriver(t)
	_, err := d.SourceActivity(context.Background(), 0)
	require.ErrorIs(t, err, snoozetypes.ErrNoTenant)
}

func TestSourceActivityTenantIsolation(t *testing.T) {
	t.Parallel()
	d := newTestDriver(t)
	ctxA := snoozetypes.WithTenant(context.Background(), "tenant-a")
	ctxB := snoozetypes.WithTenant(context.Background(), "tenant-b")

	_, err := d.Write(ctxA, "record", []dbpkg.Document{
		{"host": "a1", "source": "grafana", "date_epoch": int64(1000)},
	}, dbpkg.WriteOptions{UpdateTime: false})
	require.NoError(t, err)
	_, err = d.Write(ctxB, "record", []dbpkg.Document{
		{"host": "b1", "source": "datadog", "date_epoch": int64(2000)},
	}, dbpkg.WriteOptions{UpdateTime: false})
	require.NoError(t, err)

	rowsA, err := d.SourceActivity(ctxA, 0)
	require.NoError(t, err)
	require.Len(t, rowsA, 1)
	require.Equal(t, "grafana", rowsA[0].Source)

	rowsB, err := d.SourceActivity(ctxB, 0)
	require.NoError(t, err)
	require.Len(t, rowsB, 1)
	require.Equal(t, "datadog", rowsB[0].Source)
}
