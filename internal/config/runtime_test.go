package config

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/config/schema"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/db/sqlite"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
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

// writeSetting persists a settings row under ctx. The settings collection is
// tenant-scoped, so ctx must carry a tenant (auth.WithTenant) or platform scope
// for the fail-closed driver to accept the write; the driver stamps tenant_id.
func writeSetting(ctx context.Context, t *testing.T, d db.Driver, name string, value any) {
	t.Helper()
	_, err := d.Write(ctx, settingsCollection, []db.Document{
		{"name": name, "value": value},
	}, db.WriteOptions{Primary: []string{"name"}, UpdateTime: false})
	require.NoError(t, err)
}

// TestRuntimeSettingsLDAPOverridesBaseline locks in the layered-defaulting
// contract: file-config baseline is the starting point; DB rows that match
// “ldap.<field>“ override each field; unset keys keep the baseline.
func TestRuntimeSettingsLDAPOverridesBaseline(t *testing.T) {
	d := newDriver(t)
	ctx := auth.WithTenant(context.Background(), snoozetypes.DefaultTenant)
	baseline := Default()
	baseline.LDAP.Host = "baseline.example.com"
	baseline.LDAP.Port = 389
	baseline.LDAP.BaseDN = "dc=example,dc=com"

	writeSetting(ctx, t, d, "ldap.enabled", true)
	writeSetting(ctx, t, d, "ldap.host", "override.example.com")
	writeSetting(ctx, t, d, "ldap.port", 636)
	writeSetting(ctx, t, d, "ldap.bind_dn", "cn=svc,dc=example,dc=com")

	rs := NewRuntimeSettings(d, baseline, time.Minute)
	got, err := rs.LDAP(ctx)
	require.NoError(t, err)
	require.True(t, got.Enabled)
	require.Equal(t, "override.example.com", got.Host)
	require.Equal(t, 636, got.Port)
	require.Equal(t, "cn=svc,dc=example,dc=com", got.BindDN)
	// Untouched fields preserve the baseline.
	require.Equal(t, "dc=example,dc=com", got.BaseDN)
}

// TestRuntimeSettingsOIDCOverridesExceptSecret locks in that the OIDC connection
// + claim fields are DB-overridable, while client_secret and method stay
// file-config-only (a stray DB row for either must be ignored).
func TestRuntimeSettingsOIDCOverridesExceptSecret(t *testing.T) {
	d := newDriver(t)
	ctx := auth.WithTenant(context.Background(), snoozetypes.DefaultTenant)
	baseline := Default()
	baseline.OIDC.Method = "microsoft"
	baseline.OIDC.ClientSecret = "file-secret"
	baseline.OIDC.Issuer = "https://baseline/v2.0"

	writeSetting(ctx, t, d, "oidc.enabled", true)
	writeSetting(ctx, t, d, "oidc.issuer", "https://login.microsoftonline.com/tid/v2.0")
	writeSetting(ctx, t, d, "oidc.client_id", "cid-123")
	writeSetting(ctx, t, d, "oidc.scopes", "openid profile email User.Read")
	writeSetting(ctx, t, d, "oidc.roles_claim", "roles")
	// A stray oidc.client_secret / oidc.method row in the DB must be IGNORED —
	// the secret stays file/env, the method is fixed.
	writeSetting(ctx, t, d, "oidc.client_secret", "db-secret-should-be-ignored")
	writeSetting(ctx, t, d, "oidc.method", "evil")

	rs := NewRuntimeSettings(d, baseline, time.Minute)
	got, err := rs.OIDC(ctx)
	require.NoError(t, err)
	require.True(t, got.Enabled)
	require.Equal(t, "https://login.microsoftonline.com/tid/v2.0", got.Issuer)
	require.Equal(t, "cid-123", got.ClientID)
	require.Equal(t, []string{"openid", "profile", "email", "User.Read"}, got.Scopes)
	require.Equal(t, "file-secret", got.ClientSecret, "client_secret must never come from the DB")
	require.Equal(t, "microsoft", got.Method, "method must never come from the DB")
}

// TestRuntimeSettings_OIDCByMethod_FallsBackToBaseline: with no DB overrides,
// OIDCByMethod returns the matching baseline entry from OIDCProviders.
func TestRuntimeSettings_OIDCByMethod_FallsBackToBaseline(t *testing.T) {
	d := newDriver(t)
	ctx := auth.WithTenant(context.Background(), snoozetypes.DefaultTenant)
	baseline := Default()
	baseline.OIDCProviders = []schema.OIDC{
		{Method: "google", Provider: "google", Issuer: "https://accounts.google.com",
			ClientID: "gid", ClientSecret: "gsec"},
		{Method: "azure", Provider: "azure", Issuer: "https://login.microsoftonline.com/tid/v2.0",
			ClientID: "aid", ClientSecret: "asec"},
	}

	rs := NewRuntimeSettings(d, baseline, time.Minute)
	got, err := rs.OIDCByMethod(ctx, "azure")
	require.NoError(t, err)
	require.Equal(t, "azure", got.Method)
	require.Equal(t, "aid", got.ClientID)
	require.Equal(t, "asec", got.ClientSecret)
}

// TestRuntimeSettings_OIDCByMethod_AppliesOverride: a DB row keyed
// oidc.<method>.enabled overrides that entry's field; the client_secret stays
// file-config-only (a stray DB row must be ignored).
func TestRuntimeSettings_OIDCByMethod_AppliesOverride(t *testing.T) {
	d := newDriver(t)
	ctx := auth.WithTenant(context.Background(), snoozetypes.DefaultTenant)
	baseline := Default()
	baseline.OIDCProviders = []schema.OIDC{
		{Method: "google", Provider: "google", Issuer: "https://accounts.google.com",
			ClientID: "gid", ClientSecret: "file-secret", Enabled: false},
	}

	writeSetting(ctx, t, d, "oidc.google.enabled", true)
	writeSetting(ctx, t, d, "oidc.google.client_id", "gid-override")
	// Stray secret/method rows must be ignored.
	writeSetting(ctx, t, d, "oidc.google.client_secret", "db-secret-should-be-ignored")
	writeSetting(ctx, t, d, "oidc.google.method", "evil")

	rs := NewRuntimeSettings(d, baseline, time.Minute)
	got, err := rs.OIDCByMethod(ctx, "google")
	require.NoError(t, err)
	require.True(t, got.Enabled)
	require.Equal(t, "gid-override", got.ClientID)
	require.Equal(t, "file-secret", got.ClientSecret, "client_secret must never come from the DB")
	require.Equal(t, "google", got.Method, "method must never come from the DB")
}

// TestRuntimeSettings_OIDCByMethod_UnknownMethod: a method not present in the
// OIDCProviders slice returns ErrUnknownProvider.
func TestRuntimeSettings_OIDCByMethod_UnknownMethod(t *testing.T) {
	d := newDriver(t)
	ctx := auth.WithTenant(context.Background(), snoozetypes.DefaultTenant)
	baseline := Default()
	baseline.OIDCProviders = []schema.OIDC{
		{Method: "google", Provider: "google", Issuer: "https://accounts.google.com"},
	}

	rs := NewRuntimeSettings(d, baseline, time.Minute)
	_, err := rs.OIDCByMethod(ctx, "nope")
	require.ErrorIs(t, err, ErrUnknownProvider)
}

// TestRuntimeSettingsHousekeeperOverridesDuration locks in that string-form
// Go durations stored in the DB are parsed correctly when overlaying onto
// the schema.Duration-typed housekeeper config.
func TestRuntimeSettingsHousekeeperOverridesDuration(t *testing.T) {
	d := newDriver(t)
	ctx := auth.WithTenant(context.Background(), snoozetypes.DefaultTenant)
	writeSetting(ctx, t, d, "housekeeping.trigger_on_startup", true)
	writeSetting(ctx, t, d, "housekeeping.cleanup_snooze", "1h30m")
	writeSetting(ctx, t, d, "housekeeping.cleanup_notification", "45m")

	rs := NewRuntimeSettings(d, Default(), time.Minute)
	got, err := rs.Housekeeper(ctx)
	require.NoError(t, err)
	require.True(t, got.TriggerOnStartup)
	require.Equal(t, 90*time.Minute, got.CleanupSnooze.AsDuration())
	require.Equal(t, 45*time.Minute, got.CleanupNotification.AsDuration())
}

// TestRuntimeSettingsHousekeeperLifecycleTimeouts locks in that the timed-alert
// lifecycle tunables overlay from the DB and that the AckTimeout/EscalateAfter
// accessors surface them (with a baseline fallback for ack_timeout).
func TestRuntimeSettingsHousekeeperLifecycleTimeouts(t *testing.T) {
	d := newDriver(t)
	ctx := auth.WithTenant(context.Background(), snoozetypes.DefaultTenant)
	writeSetting(ctx, t, d, "housekeeping.ack_timeout", "2h")
	writeSetting(ctx, t, d, "housekeeping.escalate_after", "30m")

	rs := NewRuntimeSettings(d, Default(), time.Minute)
	got, err := rs.Housekeeper(ctx)
	require.NoError(t, err)
	require.Equal(t, 2*time.Hour, got.AckTimeout.AsDuration())
	require.Equal(t, 30*time.Minute, got.EscalateAfter.AsDuration())

	require.Equal(t, 2*time.Hour, rs.AckTimeout(ctx))
	require.Equal(t, 30*time.Minute, rs.EscalateAfter(ctx))
}

// TestRuntimeSettings_ShelveTimeout verifies the timed-shelve window overlays
// from the DB and that the ShelveTimeout accessor surfaces it; with no override
// it falls back to the 4h baseline.
func TestRuntimeSettings_ShelveTimeout(t *testing.T) {
	d := newDriver(t)
	ctx := auth.WithTenant(context.Background(), snoozetypes.DefaultTenant)

	// Cold start: no override → 4h baseline.
	rs := NewRuntimeSettings(d, Default(), time.Minute)
	require.Equal(t, 4*time.Hour, rs.ShelveTimeout(ctx))

	// DB override is surfaced both via the Housekeeper snapshot and the accessor.
	writeSetting(ctx, t, d, "housekeeping.shelve_timeout", "90m")
	rs2 := NewRuntimeSettings(d, Default(), time.Minute)
	got, err := rs2.Housekeeper(ctx)
	require.NoError(t, err)
	require.Equal(t, 90*time.Minute, got.ShelveTimeout.AsDuration())
	require.Equal(t, 90*time.Minute, rs2.ShelveTimeout(ctx))
}

// TestRuntimeSettings_ResolutionHold verifies the resolution-hold window
// overlays from the DB, falls back to the 2h baseline, and that an explicit 0
// (disable) is surfaced as 0 rather than replaced by the default.
func TestRuntimeSettings_ResolutionHold(t *testing.T) {
	d := newDriver(t)
	ctx := auth.WithTenant(context.Background(), snoozetypes.DefaultTenant)

	rs := NewRuntimeSettings(d, Default(), time.Minute)
	require.Equal(t, 2*time.Hour, rs.ResolutionHold(ctx))

	writeSetting(ctx, t, d, "housekeeping.resolution_hold", "45m")
	require.Equal(t, 45*time.Minute, NewRuntimeSettings(d, Default(), time.Minute).ResolutionHold(ctx))

	writeSetting(ctx, t, d, "housekeeping.resolution_hold", "0s")
	require.Equal(t, time.Duration(0), NewRuntimeSettings(d, Default(), time.Minute).ResolutionHold(ctx))
}

// TestRuntimeSettingsLifecycleTimeoutDefaults checks the cold-start fallback:
// ack_timeout defaults to 24h, escalate_after to 0 (disabled).
func TestRuntimeSettingsLifecycleTimeoutDefaults(t *testing.T) {
	d := newDriver(t)
	ctx := auth.WithTenant(context.Background(), snoozetypes.DefaultTenant)

	rs := NewRuntimeSettings(d, Default(), time.Minute)
	require.Equal(t, 24*time.Hour, rs.AckTimeout(ctx))
	require.Equal(t, time.Duration(0), rs.EscalateAfter(ctx))
}

// TestRuntimeSettingsCacheServesStaleUntilInvalidate is the contract the
// settings PATCH handler relies on: a fresh read after Set sees the new
// value only when Invalidate is called, otherwise the cache TTL governs.
func TestRuntimeSettingsCacheServesStaleUntilInvalidate(t *testing.T) {
	d := newDriver(t)
	ctx := auth.WithTenant(context.Background(), snoozetypes.DefaultTenant)
	writeSetting(ctx, t, d, "ldap.host", "v1.example.com")

	rs := NewRuntimeSettings(d, Default(), time.Hour) // long TTL, so cache wins
	first, err := rs.LDAP(ctx)
	require.NoError(t, err)
	require.Equal(t, "v1.example.com", first.Host)

	// Mutate the DB row directly and observe the cache is still serving.
	writeSetting(ctx, t, d, "ldap.host", "v2.example.com")
	cached, err := rs.LDAP(ctx)
	require.NoError(t, err)
	require.Equal(t, "v1.example.com", cached.Host, "cache should be stale before Invalidate")

	rs.Invalidate()
	refreshed, err := rs.LDAP(ctx)
	require.NoError(t, err)
	require.Equal(t, "v2.example.com", refreshed.Host)
}

// TestStatsRetention_OverrideFromDB checks that a DB override for
// "housekeeping.cleanup_stats" is surfaced by StatsRetention.
func TestStatsRetention_OverrideFromDB(t *testing.T) {
	d := newDriver(t)
	ctx := auth.WithTenant(context.Background(), snoozetypes.DefaultTenant)
	writeSetting(ctx, t, d, "housekeeping.cleanup_stats", "240h")

	rs := NewRuntimeSettings(d, Default(), time.Minute)
	require.Equal(t, 240*time.Hour, rs.StatsRetention(ctx))
}

// TestStatsRetention_FallsBackToBaseline checks that when no DB override is
// present StatsRetention returns the 400-day default.
func TestStatsRetention_FallsBackToBaseline(t *testing.T) {
	d := newDriver(t)
	ctx := auth.WithTenant(context.Background(), snoozetypes.DefaultTenant)

	rs := NewRuntimeSettings(d, Default(), time.Minute)
	require.Equal(t, 400*24*time.Hour, rs.StatsRetention(ctx))
}

// TestNotificationLogRetention_OverrideFromDB checks that a DB override for
// "housekeeping.cleanup_notificationlog" is surfaced by
// NotificationLogRetention — the knob the Settings UI writes.
func TestNotificationLogRetention_OverrideFromDB(t *testing.T) {
	d := newDriver(t)
	ctx := auth.WithTenant(context.Background(), snoozetypes.DefaultTenant)
	writeSetting(ctx, t, d, "housekeeping.cleanup_notificationlog", "168h")

	rs := NewRuntimeSettings(d, Default(), time.Minute)
	require.Equal(t, 168*time.Hour, rs.NotificationLogRetention(ctx))
}

// TestNotificationLogRetention_FallsBackToBaseline checks that with no DB
// override the 30-day (720h) file-config baseline is returned.
func TestNotificationLogRetention_FallsBackToBaseline(t *testing.T) {
	d := newDriver(t)
	ctx := auth.WithTenant(context.Background(), snoozetypes.DefaultTenant)

	rs := NewRuntimeSettings(d, Default(), time.Minute)
	require.Equal(t, 720*time.Hour, rs.NotificationLogRetention(ctx))
}

// TestDeliveryLog_OverrideFromDB is the live-toggle contract: flipping
// "notification.delivery_log" off in Settings stops the dispatcher writing
// delivery rows without a restart.
func TestDeliveryLog_OverrideFromDB(t *testing.T) {
	d := newDriver(t)
	ctx := auth.WithTenant(context.Background(), snoozetypes.DefaultTenant)

	rs := NewRuntimeSettings(d, Default(), time.Minute)
	require.True(t, rs.DeliveryLog(ctx), "baseline default should be on")

	writeSetting(ctx, t, d, "notification.delivery_log", false)
	rs.Invalidate()
	require.False(t, rs.DeliveryLog(ctx))
}

// TestNotification_SectionSnapshot verifies the Notification accessor layers
// the DB override over the file-config baseline and leaves the untouched
// fields alone.
func TestNotification_SectionSnapshot(t *testing.T) {
	d := newDriver(t)
	ctx := auth.WithTenant(context.Background(), snoozetypes.DefaultTenant)
	writeSetting(ctx, t, d, "notification.delivery_log", false)

	rs := NewRuntimeSettings(d, Default(), time.Minute)
	got, err := rs.Notification(ctx)
	require.NoError(t, err)
	require.False(t, got.DeliveryLog)
	require.True(t, got.PersistActionOutcomes, "unrelated fields keep the baseline")
	require.Equal(t, 3, got.NotificationRetry)
}

// TestRuntimeSettingsEmptyDBReturnsBaseline checks the cold-start case: no
// settings rows means every accessor returns the bootstrap baseline as-is.
func TestRuntimeSettingsEmptyDBReturnsBaseline(t *testing.T) {
	d := newDriver(t)
	ctx := auth.WithTenant(context.Background(), snoozetypes.DefaultTenant)
	baseline := Default()
	baseline.LDAP.Host = "boot.example.com"

	rs := NewRuntimeSettings(d, baseline, time.Minute)
	got, err := rs.LDAP(ctx)
	require.NoError(t, err)
	require.Equal(t, "boot.example.com", got.Host)
	require.False(t, got.Enabled)
}

// TestRuntimeSettings_TenantPartitioned verifies that LDAP overrides written
// for tenant A do not bleed into tenant B's snapshot, and that
// InvalidateForTenant clears only the affected partition.
func TestRuntimeSettings_TenantPartitioned(t *testing.T) {
	dA := newDriver(t)
	dB := newDriver(t) // separate DB to simulate tenant isolation at storage layer

	ctxA := auth.WithTenant(context.Background(), "acme")
	ctxB := auth.WithTenant(context.Background(), "beta")

	writeSetting(ctxA, t, dA, "ldap.host", "ldap-acme.example.com")
	writeSetting(ctxB, t, dB, "ldap.host", "ldap-beta.example.com")

	rs := NewRuntimeSettings(dA, Default(), time.Minute)

	gotA, err := rs.LDAP(ctxA)
	require.NoError(t, err)
	require.Equal(t, "ldap-acme.example.com", gotA.Host)

	// InvalidateForTenant evicts only tenant A; tenant B's partition is unaffected
	// (in a single-driver test this verifies the code path doesn't panic and
	// returns non-nil result after re-read).
	rs.InvalidateForTenant("acme")

	// After invalidation the next read re-fetches.
	gotA2, err := rs.LDAP(ctxA)
	require.NoError(t, err)
	require.Equal(t, "ldap-acme.example.com", gotA2.Host)
}

// TestRuntimeSettings_InvalidateAll verifies Invalidate() clears all partitions.
func TestRuntimeSettings_InvalidateAll(t *testing.T) {
	d := newDriver(t)
	ctx := auth.WithTenant(context.Background(), "acme")
	writeSetting(ctx, t, d, "ldap.host", "v1.example.com")

	rs := NewRuntimeSettings(d, Default(), time.Hour) // long TTL

	_, err := rs.LDAP(ctx)
	require.NoError(t, err)

	writeSetting(ctx, t, d, "ldap.host", "v2.example.com")
	rs.Invalidate() // clears all

	got, err := rs.LDAP(ctx)
	require.NoError(t, err)
	require.Equal(t, "v2.example.com", got.Host)
}

// TestRuntimeSettingsGeneralBaselineOnly locks in the cold-start contract: with
// no DB rows, General returns the file-config baseline defaults untouched.
func TestRuntimeSettingsGeneralBaselineOnly(t *testing.T) {
	d := newDriver(t)
	ctx := auth.WithTenant(context.Background(), snoozetypes.DefaultTenant)

	rs := NewRuntimeSettings(d, Default(), time.Minute)
	got, err := rs.General(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{"ok", "success"}, got.OKSeverities)
	require.Empty(t, got.SnoozeBySeverities)
}

// TestRuntimeSettingsGeneralOverridesSnoozeBypass verifies that a DB-stored
// snooze_bypass_severities row overrides the (empty) baseline and that the
// values are case-folded and trimmed via Normalize, matching the Python
// ok_severities validator behavior.
func TestRuntimeSettingsGeneralOverridesSnoozeBypass(t *testing.T) {
	d := newDriver(t)
	ctx := auth.WithTenant(context.Background(), snoozetypes.DefaultTenant)
	writeSetting(ctx, t, d, "snooze_bypass_severities", []string{"OK", " Critical "})

	rs := NewRuntimeSettings(d, Default(), time.Minute)
	got, err := rs.General(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{"ok", "critical"}, got.SnoozeBySeverities)
	// Untouched field preserves the baseline.
	require.Equal(t, []string{"ok", "success"}, got.OKSeverities)
}

// TestRuntimeSettingsGeneralOverridesOKSeverities verifies that a DB-stored
// ok_severities row replaces the baseline list rather than merging with it.
func TestRuntimeSettingsGeneralOverridesOKSeverities(t *testing.T) {
	d := newDriver(t)
	ctx := auth.WithTenant(context.Background(), snoozetypes.DefaultTenant)
	writeSetting(ctx, t, d, "ok_severities", []string{"OK", "Warning"})

	rs := NewRuntimeSettings(d, Default(), time.Minute)
	got, err := rs.General(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{"ok", "warning"}, got.OKSeverities)
}

// TestRuntimeSettingsGeneralNilReceiver verifies the nil-safety contract
// shared with Housekeeper/LDAP: a nil *RuntimeSettings returns the schema
// defaults without panicking.
func TestRuntimeSettingsGeneralNilReceiver(t *testing.T) {
	var rs *RuntimeSettings
	got, err := rs.General(context.Background())
	require.NoError(t, err)
	require.Equal(t, schema.DefaultGeneral(), got)
}

// TestRuntimeSettingsGeneralAcceptsCommaSeparatedString verifies that
// asStringSlice's comma/whitespace-separated string form (the wire shape a
// hand-edited settings row might use) decodes to the expected entries.
func TestRuntimeSettingsGeneralAcceptsCommaSeparatedString(t *testing.T) {
	d := newDriver(t)
	ctx := auth.WithTenant(context.Background(), snoozetypes.DefaultTenant)
	writeSetting(ctx, t, d, "ok_severities", "ok,critical")

	rs := NewRuntimeSettings(d, Default(), time.Minute)
	got, err := rs.General(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{"ok", "critical"}, got.OKSeverities)
}
