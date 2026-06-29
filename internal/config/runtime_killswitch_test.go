package config

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// TestIngestAllow_Default: a nil *RuntimeSettings (no store wired) fails open —
// ingestion is allowed.
func TestIngestAllow_Default(t *testing.T) {
	var rs *RuntimeSettings // nil receiver
	require.True(t, rs.IngestAllow(context.Background()))
}

// TestIngestAllow_Enabled: a primed RuntimeSettings with no "ingest.allow" row
// returns true (the absent key means "use the default, which is allow").
func TestIngestAllow_Enabled(t *testing.T) {
	d := newDriver(t)
	ctx := auth.WithTenant(context.Background(), snoozetypes.DefaultTenant)
	// No ingest.allow row written.
	rs := NewRuntimeSettings(d, Default(), time.Minute)
	require.True(t, rs.IngestAllow(ctx))
}

// TestIngestAllow_Disabled: an "ingest.allow": false row flips the switch off.
func TestIngestAllow_Disabled(t *testing.T) {
	d := newDriver(t)
	ctx := auth.WithTenant(context.Background(), snoozetypes.DefaultTenant)
	writeSetting(ctx, t, d, "ingest.allow", false)
	rs := NewRuntimeSettings(d, Default(), time.Minute)
	require.False(t, rs.IngestAllow(ctx))
}

// TestIngestAllow_ErrorFailsOpen: a driver whose Search returns a non-ErrNotFound
// error must NOT lock out operators — IngestAllow returns true on a read error.
func TestIngestAllow_ErrorFailsOpen(t *testing.T) {
	d := &brokenSearchDriver{Driver: newDriver(t)}
	ctx := auth.WithTenant(context.Background(), snoozetypes.DefaultTenant)
	rs := NewRuntimeSettings(d, Default(), time.Minute)
	require.True(t, rs.IngestAllow(ctx), "a settings-read error must fail open")
}

// brokenSearchDriver wraps a real driver but makes Search fail with a generic
// (non-ErrNotFound) error, exercising the fail-open path of IngestAllow.
type brokenSearchDriver struct {
	db.Driver
}

func (b *brokenSearchDriver) Search(context.Context, string, condition.Cond, db.Page) ([]db.Document, int, error) {
	return nil, 0, errors.New("simulated db failure")
}
