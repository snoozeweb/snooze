package config

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/db/sqlite"
)

// newDriver opens a per-test SQLite driver. The fresh on-disk database is
// cleaned up by t.TempDir's teardown.
func newDriver(t *testing.T) db.Driver {
	t.Helper()
	path := filepath.Join(t.TempDir(), "snooze.db")
	d, err := sqlite.New(context.Background(), sqlite.Config{Path: path})
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func writeSetting(t *testing.T, d db.Driver, name string, value any) {
	t.Helper()
	_, err := d.Write(context.Background(), settingsCollection, []db.Document{
		{"name": name, "value": value},
	}, db.WriteOptions{Primary: []string{"name"}, UpdateTime: false})
	require.NoError(t, err)
}

// TestRuntimeSettingsLDAPOverridesBaseline locks in the layered-defaulting
// contract: file-config baseline is the starting point; DB rows that match
// “ldap.<field>“ override each field; unset keys keep the baseline.
func TestRuntimeSettingsLDAPOverridesBaseline(t *testing.T) {
	d := newDriver(t)
	baseline := Default()
	baseline.LDAP.Host = "baseline.example.com"
	baseline.LDAP.Port = 389
	baseline.LDAP.BaseDN = "dc=example,dc=com"

	writeSetting(t, d, "ldap.enabled", true)
	writeSetting(t, d, "ldap.host", "override.example.com")
	writeSetting(t, d, "ldap.port", 636)
	writeSetting(t, d, "ldap.bind_dn", "cn=svc,dc=example,dc=com")

	rs := NewRuntimeSettings(d, baseline, time.Minute)
	got, err := rs.LDAP(context.Background())
	require.NoError(t, err)
	require.True(t, got.Enabled)
	require.Equal(t, "override.example.com", got.Host)
	require.Equal(t, 636, got.Port)
	require.Equal(t, "cn=svc,dc=example,dc=com", got.BindDN)
	// Untouched fields preserve the baseline.
	require.Equal(t, "dc=example,dc=com", got.BaseDN)
}

// TestRuntimeSettingsHousekeeperOverridesDuration locks in that string-form
// Go durations stored in the DB are parsed correctly when overlaying onto
// the schema.Duration-typed housekeeper config.
func TestRuntimeSettingsHousekeeperOverridesDuration(t *testing.T) {
	d := newDriver(t)
	writeSetting(t, d, "housekeeping.trigger_on_startup", true)
	writeSetting(t, d, "housekeeping.cleanup_snooze", "1h30m")
	writeSetting(t, d, "housekeeping.cleanup_notification", "45m")

	rs := NewRuntimeSettings(d, Default(), time.Minute)
	got, err := rs.Housekeeper(context.Background())
	require.NoError(t, err)
	require.True(t, got.TriggerOnStartup)
	require.Equal(t, 90*time.Minute, got.CleanupSnooze.AsDuration())
	require.Equal(t, 45*time.Minute, got.CleanupNotification.AsDuration())
}

// TestRuntimeSettingsCacheServesStaleUntilInvalidate is the contract the
// settings PATCH handler relies on: a fresh read after Set sees the new
// value only when Invalidate is called, otherwise the cache TTL governs.
func TestRuntimeSettingsCacheServesStaleUntilInvalidate(t *testing.T) {
	d := newDriver(t)
	writeSetting(t, d, "ldap.host", "v1.example.com")

	rs := NewRuntimeSettings(d, Default(), time.Hour) // long TTL, so cache wins
	first, err := rs.LDAP(context.Background())
	require.NoError(t, err)
	require.Equal(t, "v1.example.com", first.Host)

	// Mutate the DB row directly and observe the cache is still serving.
	writeSetting(t, d, "ldap.host", "v2.example.com")
	cached, err := rs.LDAP(context.Background())
	require.NoError(t, err)
	require.Equal(t, "v1.example.com", cached.Host, "cache should be stale before Invalidate")

	rs.Invalidate()
	refreshed, err := rs.LDAP(context.Background())
	require.NoError(t, err)
	require.Equal(t, "v2.example.com", refreshed.Host)
}

// TestStatsRetention_OverrideFromDB checks that a DB override for
// "housekeeping.cleanup_stats" is surfaced by StatsRetention.
func TestStatsRetention_OverrideFromDB(t *testing.T) {
	d := newDriver(t)
	writeSetting(t, d, "housekeeping.cleanup_stats", "240h")

	rs := NewRuntimeSettings(d, Default(), time.Minute)
	require.Equal(t, 240*time.Hour, rs.StatsRetention(context.Background()))
}

// TestStatsRetention_FallsBackToBaseline checks that when no DB override is
// present StatsRetention returns the 400-day default.
func TestStatsRetention_FallsBackToBaseline(t *testing.T) {
	d := newDriver(t)

	rs := NewRuntimeSettings(d, Default(), time.Minute)
	require.Equal(t, 400*24*time.Hour, rs.StatsRetention(context.Background()))
}

// TestRuntimeSettingsEmptyDBReturnsBaseline checks the cold-start case: no
// settings rows means every accessor returns the bootstrap baseline as-is.
func TestRuntimeSettingsEmptyDBReturnsBaseline(t *testing.T) {
	d := newDriver(t)
	baseline := Default()
	baseline.LDAP.Host = "boot.example.com"

	rs := NewRuntimeSettings(d, baseline, time.Minute)
	got, err := rs.LDAP(context.Background())
	require.NoError(t, err)
	require.Equal(t, "boot.example.com", got.Host)
	require.False(t, got.Enabled)
}
