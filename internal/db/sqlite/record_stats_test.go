package sqlite

import (
	"context"
	"testing"
	"time"

	dbpkg "github.com/snoozeweb/snooze/internal/db"
	"github.com/stretchr/testify/require"
)

func TestRecordStats(t *testing.T) {
	t.Parallel()
	d := newTestDriver(t)
	ctx := context.Background()

	// Seed: 4 records at known epochs (UTC). The bucket window is 60s so we
	// know exactly which slot each row lands in.
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC).Unix()
	rows := []dbpkg.Document{
		{"host": "srv-prod-1", "severity": "critical", "environment": "production", "source": "prometheus", "date_epoch": base},
		{"host": "srv-prod-2", "severity": "warning", "environment": "production", "source": "prometheus", "date_epoch": base + 5},
		{"host": "srv-stage-1", "severity": "info", "environment": "", "source": "ci", "date_epoch": base + 65},
		{"host": "out-of-window", "severity": "critical", "environment": "production", "source": "prometheus", "date_epoch": base - 600},
	}
	// UpdateTime would overwrite date_epoch with time.Now(); preserve the
	// seeded epochs so the bucket assertions are deterministic.
	_, err := d.Write(ctx, "record", rows, dbpkg.WriteOptions{UpdateTime: false})
	require.NoError(t, err)

	from := time.Unix(base, 0).UTC()
	to := time.Unix(base+120, 0).UTC()

	res, err := d.RecordStats(ctx, from, to, 60)
	require.NoError(t, err)

	// Slot 1: 2 prometheus rows at base / base+5
	require.Equal(t, int64(2), res.Series[base/60*60]["prometheus"])
	// Slot 2: 1 ci row at base+65
	require.Equal(t, int64(1), res.Series[(base+65)/60*60]["ci"])
	// Out-of-window row excluded from totals — 3 in window, one each.
	require.Equal(t, int64(1), res.BySeverity["critical"])
	require.Equal(t, int64(1), res.BySeverity["warning"])
	require.Equal(t, int64(1), res.BySeverity["info"])
	// Empty-string environment normalises to "(none)"
	require.Equal(t, int64(2), res.ByEnvironment["production"])
	require.Equal(t, int64(1), res.ByEnvironment["(none)"])
}

func TestRecordStatsEmptyCollection(t *testing.T) {
	t.Parallel()
	d := newTestDriver(t)
	res, err := d.RecordStats(context.Background(), time.Now().Add(-time.Hour), time.Now(), 60)
	require.NoError(t, err)
	require.Empty(t, res.Series)
	require.Empty(t, res.BySeverity)
	require.Empty(t, res.ByEnvironment)
}
