package housekeeper

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/db"
)

// TestReconcileSuppressionJob_EveryTenantEveryMinute: the sweep that returns
// rows to the alerts list once the filter that silenced them can no longer
// silence anything runs on a fixed minute cadence (a "snooze for 30m" must end
// close to on time) and visits every active tenant — one tenant failing must
// not leave the others' rows hidden.
func TestReconcileSuppressionJob_EveryTenantEveryMinute(t *testing.T) {
	drv := newEscalateFakeDriver()
	drv.tenants = []db.Document{
		{"id": "default", "status": "active"},
		{"id": "broken", "status": "active"},
		{"id": "acme", "status": "active"},
		{"id": "frozen", "status": "suspended"},
	}
	var visited []string
	reconcile := func(ctx context.Context) (int, error) {
		id := tenantOf(ctx)
		visited = append(visited, id)
		if id == "broken" {
			return 0, errors.New("boom")
		}
		return 1, nil
	}

	ij := ReconcileSuppressionJob(drv, reconcile)
	require.Equal(t, time.Minute, ij.Interval)
	require.Equal(t, "reconcile_suppression", ij.Job.Name())

	err := ij.Job.Run(context.Background())
	require.ErrorContains(t, err, "boom")
	require.Equal(t, []string{"default", "broken", "acme"}, visited)
}
