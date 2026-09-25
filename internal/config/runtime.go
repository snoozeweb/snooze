package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/config/schema"
	"github.com/snoozeweb/snooze/internal/db"
)

// RuntimeStore is the contract for the live-editable counterpart of the
// bootstrap Config. It replaces Python's filelock-protected “WritableConfig“
// hierarchy. The settings plugin is the production implementation; tests
// can use “NoopRuntimeStore“ or a hand-rolled fake.
//
// Implementations MUST be safe for concurrent use. Updates are expected to
// notify subscribers via Watch so that components like the LDAP backend or
// the notification worker can pick up changes without a restart.
type RuntimeStore interface {
	// Get returns the value stored under section.key. The bool is false if the
	// key has never been written.
	Get(ctx context.Context, section, key string) (any, bool, error)

	// GetSection unmarshals the full section into dst (typically a pointer to
	// the matching ``schema.*`` struct).
	GetSection(ctx context.Context, section string, dst any) error

	// Set persists a single key/value pair. The implementation is free to
	// reject unknown keys.
	Set(ctx context.Context, section, key string, value any) error

	// Replace overwrites the entire section with values. Useful for the
	// "import settings" use case.
	Replace(ctx context.Context, section string, values map[string]any) error

	// Watch returns a channel that receives a notification each time the
	// section changes. The channel is closed when ctx is cancelled.
	Watch(ctx context.Context, section string) (<-chan RuntimeChange, error)
}

// RuntimeChange describes a single mutation event delivered through
// “RuntimeStore.Watch“.
type RuntimeChange struct {
	Section string
	Key     string
	Value   any
}

// NoopRuntimeStore is a minimal implementation that returns "not found" for
// every read and accepts every write silently. It exists so that callers
// can exercise the Config struct in isolation (in tests, in the migration
// tool, etc.) before the settings plugin is wired in.
type NoopRuntimeStore struct{}

// Get implements RuntimeStore.
func (NoopRuntimeStore) Get(context.Context, string, string) (any, bool, error) {
	return nil, false, nil
}

// GetSection implements RuntimeStore.
func (NoopRuntimeStore) GetSection(context.Context, string, any) error { return nil }

// Set implements RuntimeStore.
func (NoopRuntimeStore) Set(context.Context, string, string, any) error { return nil }

// Replace implements RuntimeStore.
func (NoopRuntimeStore) Replace(context.Context, string, map[string]any) error { return nil }

// Watch implements RuntimeStore; it returns a closed channel immediately.
func (NoopRuntimeStore) Watch(_ context.Context, _ string) (<-chan RuntimeChange, error) {
	ch := make(chan RuntimeChange)
	close(ch)
	return ch, nil
}

// NoopRuntimeSettings is kept as an alias for the old interface fake used by
// the test fixtures that predate the RuntimeStore/RuntimeSettings split.
//
// Deprecated: use “NoopRuntimeStore“ directly.
type NoopRuntimeSettings = NoopRuntimeStore

// LDAPConfig is the runtime-readable LDAP configuration snapshot. Same field
// set as “schema.LDAP“ but isolated from the file-config baseline so that
// other packages can depend on this type without importing the schema
// package.
type LDAPConfig = schema.LDAP

// HousekeeperConfig is the runtime-readable housekeeper configuration
// snapshot. Same field set as “schema.Housekeeper“.
type HousekeeperConfig = schema.Housekeeper

// NotificationConfig is the runtime-readable notification-dispatcher
// configuration snapshot. Same field set as “schema.Notification“.
type NotificationConfig = schema.Notification

// OIDCConfig is the runtime-readable OIDC configuration snapshot. Same field
// set as “schema.OIDC“; the “client_secret“ and “method“ fields are never
// sourced from the DB (see applyOIDCOverrides).
type OIDCConfig = schema.OIDC

// settingsCollection is the DB collection holding the flat key/value
// catalogue rows (“{uid, name, value, comment}“) the React Settings page
// writes to.
const settingsCollection = "settings"

// ErrUnknownProvider is returned by OIDCByMethod when the requested method is
// not present in the baseline OIDCProviders slice.
var ErrUnknownProvider = errors.New("unknown OIDC provider method")

// RuntimeSettings reads DB-backed settings with a small read-through cache.
// The cache is tenant-partitioned: each tenant gets its own snapshot, keyed by
// tenant slug. The PATCH/POST/PUT/DELETE handler for the “settings“ collection
// calls Invalidate (all partitions) or InvalidateForTenant (one partition) so
// an edit in the UI takes effect on the next read for that tenant.
//
// Layered defaulting: the bootstrap Config provides the baseline; values
// stored in the DB override per-key. Keys use the dotted-prefix
// convention (“ldap.host“, “housekeeping.cleanup_snooze“).
type RuntimeSettings struct {
	drv      db.Driver
	baseline *Config
	cacheTTL time.Duration

	mu         sync.RWMutex
	partitions map[string]*settingsPartition // tenantID → partition
}

type settingsPartition struct {
	cached    map[string]any
	cachedAt  time.Time
	expiresAt time.Time
}

// NewRuntimeSettings constructs a RuntimeSettings reading from drv with the
// supplied cache TTL. Baseline supplies the file-config defaults; a nil
// baseline is treated as an empty Config.
func NewRuntimeSettings(drv db.Driver, baseline *Config, cacheTTL time.Duration) *RuntimeSettings {
	if cacheTTL <= 0 {
		cacheTTL = 5 * time.Second
	}
	if baseline == nil {
		baseline = Default()
	}
	return &RuntimeSettings{
		drv:        drv,
		baseline:   baseline,
		cacheTTL:   cacheTTL,
		partitions: make(map[string]*settingsPartition),
	}
}

// Invalidate forces the next read for every tenant to refresh from the DB.
// Safe to call concurrently with any other RuntimeSettings method.
func (r *RuntimeSettings) Invalidate() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.partitions = make(map[string]*settingsPartition)
	r.mu.Unlock()
}

// InvalidateForTenant forces the next read for tenantID to refresh from the DB.
// Other tenants' cached partitions are unaffected.
func (r *RuntimeSettings) InvalidateForTenant(tenantID string) {
	if r == nil || tenantID == "" {
		return
	}
	r.mu.Lock()
	delete(r.partitions, tenantID)
	r.mu.Unlock()
}

// LDAP returns the current LDAP configuration. The baseline “cfg.LDAP“ is
// the starting point; any DB-stored “ldap.*“ keys override the matching
// fields. The returned value is a copy; callers may mutate it freely.
func (r *RuntimeSettings) LDAP(ctx context.Context) (LDAPConfig, error) {
	if r == nil {
		return schema.DefaultLDAP(), nil
	}
	values, err := r.load(ctx)
	if err != nil {
		return LDAPConfig{}, err
	}
	out := r.baseline.LDAP
	applyLDAPOverrides(&out, values)
	return out, nil
}

// OIDC returns the current OIDC configuration: the file-config baseline with
// any DB-stored “oidc.*“ keys overlaid. The “client_secret“ and “method“ are
// intentionally NOT overridable from the DB — the secret stays a file/env
// secret, and the method is the login URL segment + identity claim. The
// returned value is a copy; callers may mutate it freely.
func (r *RuntimeSettings) OIDC(ctx context.Context) (OIDCConfig, error) {
	if r == nil {
		return schema.DefaultOIDC(), nil
	}
	values, err := r.load(ctx)
	if err != nil {
		return OIDCConfig{}, err
	}
	out := r.baseline.OIDC
	applyOIDCOverrides(&out, values)
	return out, nil
}

// OIDCByMethod returns the live config for one entry of the multi-IdP
// OIDCProviders slice, identified by its method slug. The baseline entry is the
// starting point; any DB-stored “oidc.<method>.*“ keys overlay the matching
// fields. As with the single-IdP OIDC path, “client_secret“ and “method“ are
// NEVER sourced from the DB — the secret stays a file/env secret and the method
// is the login URL segment + identity claim. An unknown method returns
// ErrUnknownProvider. The returned value is a copy; callers may mutate it.
func (r *RuntimeSettings) OIDCByMethod(ctx context.Context, method string) (OIDCConfig, error) {
	if r == nil {
		return OIDCConfig{}, ErrUnknownProvider
	}
	var (
		out   schema.OIDC
		found bool
	)
	for _, e := range r.baseline.OIDCProviders {
		if e.Method == method {
			out = e
			found = true
			break
		}
	}
	if !found {
		return OIDCConfig{}, fmt.Errorf("%w: %q", ErrUnknownProvider, method)
	}
	values, err := r.load(ctx)
	if err != nil {
		return OIDCConfig{}, err
	}
	applyOIDCOverridesByMethod(&out, method, values)
	return out, nil
}

// AuditRetention returns the current value of housekeeping.cleanup_audit, or
// the file-config baseline when no DB override is set. Returns zero if both
// are unset. Implements the narrow “auditRetention“ contract expected by
// the housekeeper's CleanupAuditAsIntervalJob.
func (r *RuntimeSettings) AuditRetention(ctx context.Context) time.Duration {
	if r == nil {
		return 0
	}
	hk, err := r.Housekeeper(ctx)
	if err != nil {
		return 0
	}
	return hk.CleanupAudit.AsDuration()
}

// NotificationLogRetention returns the current value of
// housekeeping.cleanup_notificationlog, or the file-config baseline when no DB
// override is set. Returns zero if both are unset. Implements the narrow
// “notificationLogRetention“ contract expected by the housekeeper's
// CleanupNotificationLogAsIntervalJob, which substitutes its own 720h fallback
// on zero. Mirrors AuditRetention exactly.
func (r *RuntimeSettings) NotificationLogRetention(ctx context.Context) time.Duration {
	if r == nil {
		return 0
	}
	hk, err := r.Housekeeper(ctx)
	if err != nil {
		return 0
	}
	return hk.CleanupNotificationLog.AsDuration()
}

// StatsRetention returns the current housekeeping.cleanup_stats window, or the
// 400-day baseline when unset. Consumed by the cleanup_stats housekeeper job.
func (r *RuntimeSettings) StatsRetention(ctx context.Context) time.Duration {
	if r == nil {
		return schema.DefaultHousekeeper().CleanupStats.AsDuration()
	}
	hk, err := r.Housekeeper(ctx)
	if err != nil {
		return schema.DefaultHousekeeper().CleanupStats.AsDuration()
	}
	return hk.CleanupStats.AsDuration()
}

// AckTimeout returns the current housekeeping.ack_timeout window — how long an
// acknowledgement holds before the escalate-timeout sweep reverts the record to
// "open". Falls back to the 24h baseline when unset or on a read error.
// Implements the narrow lifecycleTimeouts contract the housekeeper's
// EscalateTimeoutJob expects.
func (r *RuntimeSettings) AckTimeout(ctx context.Context) time.Duration {
	fallback := schema.DefaultHousekeeper().AckTimeout.AsDuration()
	if r == nil {
		return fallback
	}
	hk, err := r.Housekeeper(ctx)
	if err != nil {
		return fallback
	}
	if d := hk.AckTimeout.AsDuration(); d > 0 {
		return d
	}
	return fallback
}

// EscalateAfter returns the current housekeeping.escalate_after window — how
// long an un-acknowledged open alert may sit before auto-escalating to "esc".
// A zero (or unset, or unreadable) value means auto-escalation is DISABLED, so
// this deliberately returns 0 in those cases rather than substituting a
// non-zero default; the sweep treats <=0 as a no-op. Implements the narrow
// lifecycleTimeouts contract the housekeeper's EscalateTimeoutJob expects.
func (r *RuntimeSettings) EscalateAfter(ctx context.Context) time.Duration {
	if r == nil {
		return 0
	}
	hk, err := r.Housekeeper(ctx)
	if err != nil {
		return 0
	}
	return hk.EscalateAfter.AsDuration()
}

// ShelveTimeout returns the current housekeeping.shelve_timeout window — how
// long a time-boxed shelve lasts before the unshelve-timeout sweep reverts the
// record from "shelved" to "open". Falls back to the 4h baseline when unset or
// on a read error. Mirrors AckTimeout; the comment plugin reads this to stamp
// shelve_until.
func (r *RuntimeSettings) ShelveTimeout(ctx context.Context) time.Duration {
	fallback := schema.DefaultHousekeeper().ShelveTimeout.AsDuration()
	if r == nil {
		return fallback
	}
	hk, err := r.Housekeeper(ctx)
	if err != nil {
		return fallback
	}
	if d := hk.ShelveTimeout.AsDuration(); d > 0 {
		return d
	}
	return fallback
}

// ResolutionHold returns the current housekeeping.resolution_hold window — how
// long a re-fire keeps an alert an operator closed as fixed closed (see
// internal/resolutionhold). Unlike ShelveTimeout a zero is meaningful: it
// disables the hold. Falls back to the file-config baseline on a read error.
func (r *RuntimeSettings) ResolutionHold(ctx context.Context) time.Duration {
	fallback := schema.DefaultHousekeeper().ResolutionHold.AsDuration()
	if r == nil {
		return fallback
	}
	hk, err := r.Housekeeper(ctx)
	if err != nil {
		return fallback
	}
	return hk.ResolutionHold.AsDuration()
}

// Notification returns the current notification-dispatcher configuration: the
// file-config baseline with any DB-stored “notification.*“ keys overlaid. Same
// layering as Housekeeper. The returned value is a copy.
func (r *RuntimeSettings) Notification(ctx context.Context) (NotificationConfig, error) {
	if r == nil {
		return schema.DefaultNotification(), nil
	}
	values, err := r.load(ctx)
	if err != nil {
		return NotificationConfig{}, err
	}
	out := r.baseline.Notification
	applyNotificationOverrides(&out, values)
	return out, nil
}

// DeliveryLog reports whether the dispatcher should write a delivery-history
// row per send into the “notificationlog“ collection. It is the live-override
// counterpart of the file-config “notification.delivery_log“ flag, so an
// operator toggling it in Settings -> Notifications takes effect on the next
// send without a restart.
//
// It deliberately fails OPEN (returns the baseline default “true“) on a nil
// receiver or a settings-read error: a DB hiccup must not silently lose the
// delivery history.
func (r *RuntimeSettings) DeliveryLog(ctx context.Context) bool {
	if r == nil {
		return schema.DefaultNotification().DeliveryLog
	}
	n, err := r.Notification(ctx)
	if err != nil {
		return r.baseline.Notification.DeliveryLog
	}
	return n.DeliveryLog
}

// applyNotificationOverrides overlays the dotted-prefix DB values onto a
// baseline Notification config. Only “notification.delivery_log“ is
// runtime-editable today: notification_freq / notification_retry are parsed
// but unimplemented (they keep their legacy FLAT settings keys and no overlay),
// and persist_action_outcomes stays a file-config-only write-pressure knob.
// Unknown keys are ignored.
func applyNotificationOverrides(out *schema.Notification, values map[string]any) {
	if v, ok := values["notification.delivery_log"]; ok {
		if b, ok := asBool(v); ok {
			out.DeliveryLog = b
		}
	}
}

// Housekeeper returns the current housekeeper configuration with the same
// "baseline + DB overrides" layering as LDAP.
func (r *RuntimeSettings) Housekeeper(ctx context.Context) (HousekeeperConfig, error) {
	if r == nil {
		return schema.DefaultHousekeeper(), nil
	}
	values, err := r.load(ctx)
	if err != nil {
		return HousekeeperConfig{}, err
	}
	out := r.baseline.Housekeeper
	applyHousekeeperOverrides(&out, values)
	return out, nil
}

// General returns the current general configuration with the same "baseline +
// DB overrides" layering as Housekeeper. The severity lists are deep-copied
// before Normalize runs: `out := r.baseline.General` copies the struct but
// shares the OKSeverities/SnoozeBySeverities backing arrays with the
// baseline, and Normalize case-folds those slices in place — without the
// copy, a read with no DB override would rewrite the baseline's own backing
// array (harmless today since the baseline was normalized at load time and
// the writes are idempotent, but a lurking hazard if that ever changes).
func (r *RuntimeSettings) General(ctx context.Context) (schema.General, error) {
	if r == nil {
		return schema.DefaultGeneral(), nil
	}
	values, err := r.load(ctx)
	if err != nil {
		return schema.General{}, err
	}
	out := r.baseline.General
	out.OKSeverities = append([]string(nil), out.OKSeverities...)
	out.SnoozeBySeverities = append([]string(nil), out.SnoozeBySeverities...)
	applyGeneralOverrides(&out, values)
	out.Normalize()
	return out, nil
}

// IngestAllow reports whether alert ingestion is currently permitted for the
// tenant in ctx. It is the runtime kill-switch read by the HTTP edge guards on
// POST /api/v1/alerts and the webhook receivers. The file-config baseline
// (schema.Ingest.Allow, default true) is the starting point; a DB-stored
// "ingest.allow" row overrides it per-tenant.
//
// It deliberately fails OPEN: a nil receiver, a missing key, or any
// settings-read error all return true. A DB hiccup must never silently lock
// operators out of intake.
func (r *RuntimeSettings) IngestAllow(ctx context.Context) bool {
	if r == nil {
		return true
	}
	values, err := r.load(ctx)
	if err != nil {
		return true
	}
	out := r.baseline.Ingest
	applyIngestOverrides(&out, values)
	return out.Allow
}

// applyIngestOverrides overlays the dotted-prefix DB values onto a baseline
// Ingest config. Only the runtime kill-switch (ingest.allow) is overridable;
// the hardening fields (token, sns_verify, sentry_secret) stay file-config
// only — they are infra secrets, not ops toggles. Unknown keys are ignored.
func applyIngestOverrides(out *schema.Ingest, values map[string]any) {
	if v, ok := values["ingest.allow"]; ok {
		if b, ok := asBool(v); ok {
			out.Allow = b
		}
	}
}

// applyGeneralOverrides overlays the FLAT (non-dotted) DB values onto a
// baseline General config. Only the two severity lists — ok_severities and
// snooze_bypass_severities — are overridable; the auth-backend toggles
// (default_auth_backend, local_enabled, anonymous_enabled, …) stay
// file-config only, since they gate login itself and are set up at
// deploy/bootstrap time rather than tuned from the settings UI. Unknown keys
// are ignored.
func applyGeneralOverrides(out *schema.General, values map[string]any) {
	if v, ok := values["ok_severities"]; ok {
		if ss, ok := asStringSlice(v); ok {
			out.OKSeverities = ss
		}
	}
	if v, ok := values["snooze_bypass_severities"]; ok {
		if ss, ok := asStringSlice(v); ok {
			out.SnoozeBySeverities = ss
		}
	}
}

// Get returns the raw DB-stored value for a flat key, or the second return
// value `false` when the key isn't in the catalogue. Useful for ad-hoc
// access from places that don't have a typed schema struct.
func (r *RuntimeSettings) Get(ctx context.Context, key string) (any, bool, error) {
	if r == nil {
		return nil, false, nil
	}
	values, err := r.load(ctx)
	if err != nil {
		return nil, false, err
	}
	v, ok := values[key]
	return v, ok, nil
}

// load fetches the cached catalogue for the tenant in ctx, or refreshes it.
// Uses platform scope as a fallback when no tenant is set (housekeeper, admin):
// the empty-string partition serves baseline-only values with no DB override.
// We hold an RLock for the cache-hit fast path and upgrade to a Lock only when
// the partition is missing or expired.
func (r *RuntimeSettings) load(ctx context.Context) (map[string]any, error) {
	tenantID, _ := auth.TenantFrom(ctx)
	// Under platform scope or no-tenant contexts use the empty-string partition
	// (baseline-only; no DB override applied for cross-tenant reads).
	if tenantID == "" {
		return map[string]any{}, nil
	}

	r.mu.RLock()
	part, ok := r.partitions[tenantID]
	if ok && time.Now().Before(part.expiresAt) {
		out := part.cached
		r.mu.RUnlock()
		return out, nil
	}
	r.mu.RUnlock()

	r.mu.Lock()
	defer r.mu.Unlock()
	// Re-check under the write lock — another goroutine may have refreshed.
	if part, ok = r.partitions[tenantID]; ok && time.Now().Before(part.expiresAt) {
		return part.cached, nil
	}
	values, err := r.readAll(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	r.partitions[tenantID] = &settingsPartition{
		cached:    values,
		cachedAt:  now,
		expiresAt: now.Add(r.cacheTTL),
	}
	return values, nil
}

// readAll scans every row in the settings collection and folds it into a
// flat map keyed by `name`. The driver injects tenant_id via ctx automatically.
// Missing collection / empty collection both return an empty map.
func (r *RuntimeSettings) readAll(ctx context.Context) (map[string]any, error) {
	if r.drv == nil {
		return map[string]any{}, nil
	}
	docs, _, err := r.drv.Search(ctx, settingsCollection, condition.Cond{}, db.Page{})
	if err != nil {
		// Missing collection is fine — no overrides yet.
		if errors.Is(err, db.ErrNotFound) {
			return map[string]any{}, nil
		}
		return nil, fmt.Errorf("runtime settings: read: %w", err)
	}
	out := make(map[string]any, len(docs))
	for _, d := range docs {
		name, _ := d["name"].(string)
		if name == "" {
			continue
		}
		out[name] = d["value"]
	}
	return out, nil
}

// applyLDAPOverrides overlays the dotted-prefix DB values onto a baseline
// LDAPConfig. Unknown keys are ignored.
func applyLDAPOverrides(out *LDAPConfig, values map[string]any) {
	if v, ok := values["ldap.enabled"]; ok {
		if b, ok := asBool(v); ok {
			out.Enabled = b
		}
	}
	if v, ok := values["ldap.host"]; ok {
		if s, ok := asString(v); ok {
			out.Host = s
		}
	}
	if v, ok := values["ldap.port"]; ok {
		if n, ok := asInt(v); ok {
			out.Port = n
		}
	}
	if v, ok := values["ldap.bind_dn"]; ok {
		if s, ok := asString(v); ok {
			out.BindDN = s
		}
	}
	if v, ok := values["ldap.bind_password"]; ok {
		if s, ok := asString(v); ok {
			out.BindPassword = s
		}
	}
	if v, ok := values["ldap.base_dn"]; ok {
		if s, ok := asString(v); ok {
			out.BaseDN = s
		}
	}
	if v, ok := values["ldap.user_filter"]; ok {
		if s, ok := asString(v); ok {
			out.UserFilter = s
		}
	}
	if v, ok := values["ldap.display_name_attribute"]; ok {
		if s, ok := asString(v); ok {
			out.DisplayNameAttribute = s
		}
	}
	if v, ok := values["ldap.email_attribute"]; ok {
		if s, ok := asString(v); ok {
			out.EmailAttribute = s
		}
	}
	if v, ok := values["ldap.member_attribute"]; ok {
		if s, ok := asString(v); ok {
			out.MemberAttribute = s
		}
	}
	if v, ok := values["ldap.group_dn"]; ok {
		if s, ok := asString(v); ok {
			out.GroupDN = s
		}
	}
}

// applyOIDCOverrides overlays the dotted-prefix DB values onto a baseline OIDC
// config. The connection + claim-mapping fields are runtime-editable; the
// client_secret and method are deliberately left untouched (secret stays in
// the file/env, method is fixed). scopes is stored as a space-separated string
// (mirroring grafana.ini's "scopes = openid email profile"). Unknown keys are
// ignored.
func applyOIDCOverrides(out *schema.OIDC, values map[string]any) {
	if v, ok := values["oidc.enabled"]; ok {
		if b, ok := asBool(v); ok {
			out.Enabled = b
		}
	}
	if v, ok := values["oidc.issuer"]; ok {
		if s, ok := asString(v); ok {
			out.Issuer = s
		}
	}
	if v, ok := values["oidc.client_id"]; ok {
		if s, ok := asString(v); ok {
			out.ClientID = s
		}
	}
	if v, ok := values["oidc.redirect_url"]; ok {
		if s, ok := asString(v); ok {
			out.RedirectURL = s
		}
	}
	if v, ok := values["oidc.scopes"]; ok {
		if ss, ok := asStringSlice(v); ok {
			out.Scopes = ss
		}
	}
	if v, ok := values["oidc.roles_claim"]; ok {
		if s, ok := asString(v); ok {
			out.RolesClaim = s
		}
	}
	if v, ok := values["oidc.groups_claim"]; ok {
		if s, ok := asString(v); ok {
			out.GroupsClaim = s
		}
	}
}

// applyOIDCOverridesByMethod overlays the per-method dotted-prefix DB values
// (“oidc.<method>.enabled“, “oidc.<method>.issuer“, …) onto a baseline OIDC
// entry. It mirrors applyOIDCOverrides exactly — same overridable field set,
// and the client_secret + method are deliberately NOT taken from the DB (the
// secret stays file/env, the method is the fixed login URL segment + claim).
func applyOIDCOverridesByMethod(out *schema.OIDC, method string, values map[string]any) {
	prefix := "oidc." + method + "."
	if v, ok := values[prefix+"enabled"]; ok {
		if b, ok := asBool(v); ok {
			out.Enabled = b
		}
	}
	if v, ok := values[prefix+"issuer"]; ok {
		if s, ok := asString(v); ok {
			out.Issuer = s
		}
	}
	if v, ok := values[prefix+"client_id"]; ok {
		if s, ok := asString(v); ok {
			out.ClientID = s
		}
	}
	if v, ok := values[prefix+"redirect_url"]; ok {
		if s, ok := asString(v); ok {
			out.RedirectURL = s
		}
	}
	if v, ok := values[prefix+"scopes"]; ok {
		if ss, ok := asStringSlice(v); ok {
			out.Scopes = ss
		}
	}
	if v, ok := values[prefix+"roles_claim"]; ok {
		if s, ok := asString(v); ok {
			out.RolesClaim = s
		}
	}
	if v, ok := values[prefix+"groups_claim"]; ok {
		if s, ok := asString(v); ok {
			out.GroupsClaim = s
		}
	}
}

// applyHousekeeperOverrides overlays the dotted-prefix DB values onto a
// baseline HousekeeperConfig. Durations are passed through schema.Duration's
// text unmarshaller so the same wire format works for both file-config and
// runtime entries.
func applyHousekeeperOverrides(out *HousekeeperConfig, values map[string]any) {
	if v, ok := values["housekeeping.trigger_on_startup"]; ok {
		if b, ok := asBool(v); ok {
			out.TriggerOnStartup = b
		}
	}
	overlayDuration(values, "housekeeping.record_ttl", &out.RecordTTL)
	overlayDuration(values, "housekeeping.cleanup_alert", &out.CleanupAlert)
	overlayDuration(values, "housekeeping.cleanup_aggregate", &out.CleanupAggregate)
	overlayDuration(values, "housekeeping.cleanup_comment", &out.CleanupComment)
	overlayDuration(values, "housekeeping.cleanup_snooze", &out.CleanupSnooze)
	overlayDuration(values, "housekeeping.cleanup_notification", &out.CleanupNotification)
	overlayDuration(values, "housekeeping.cleanup_notificationlog", &out.CleanupNotificationLog)
	overlayDuration(values, "housekeeping.cleanup_audit", &out.CleanupAudit)
	overlayDuration(values, "housekeeping.cleanup_stats", &out.CleanupStats)
	overlayDuration(values, "housekeeping.cleanup_orphans", &out.CleanupOrphans)
	overlayDuration(values, "housekeeping.cleanup_apikey", &out.CleanupAPIKey)
	overlayDuration(values, "housekeeping.cleanup_refresh_token", &out.CleanupRefreshToken)
	overlayDuration(values, "housekeeping.ack_timeout", &out.AckTimeout)
	overlayDuration(values, "housekeeping.escalate_after", &out.EscalateAfter)
	overlayDuration(values, "housekeeping.shelve_timeout", &out.ShelveTimeout)
	overlayDuration(values, "housekeeping.resolution_hold", &out.ResolutionHold)
}

// overlayDuration writes the value at key into dst, parsing the
// settings-typed string form ("5m", "172800s") or a bare number-of-seconds.
// Unparseable values are ignored to keep one bad row from breaking the rest
// of the snapshot.
func overlayDuration(values map[string]any, key string, dst *schema.Duration) {
	v, ok := values[key]
	if !ok {
		return
	}
	switch x := v.(type) {
	case string:
		if x == "" {
			return
		}
		_ = dst.UnmarshalText([]byte(x))
	case float64:
		*dst = schema.Duration(time.Duration(x * float64(time.Second)))
	case int:
		*dst = schema.Duration(time.Duration(x) * time.Second)
	case int64:
		*dst = schema.Duration(time.Duration(x) * time.Second)
	case json.Number:
		if f, err := x.Float64(); err == nil {
			*dst = schema.Duration(time.Duration(f * float64(time.Second)))
		}
	}
}

func asBool(v any) (bool, bool) {
	switch x := v.(type) {
	case bool:
		return x, true
	case string:
		s := strings.ToLower(strings.TrimSpace(x))
		switch s {
		case "true", "1", "yes", "on":
			return true, true
		case "false", "0", "no", "off":
			return false, true
		}
	}
	return false, false
}

func asString(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case fmt.Stringer:
		return x.String(), true
	}
	return "", false
}

// asStringSlice coerces a settings value into a []string. It accepts a native
// list ([]string / []any) or a single string split on whitespace and commas —
// so "openid profile email" and "openid,profile,email" both work.
func asStringSlice(v any) ([]string, bool) {
	switch x := v.(type) {
	case []string:
		return x, true
	case []any:
		out := make([]string, 0, len(x))
		for _, e := range x {
			if s, ok := e.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out, true
	case string:
		fields := strings.FieldsFunc(x, func(r rune) bool { return r == ' ' || r == ',' || r == '\t' || r == '\n' })
		if len(fields) == 0 {
			return nil, false
		}
		return fields, true
	}
	return nil, false
}

func asInt(v any) (int, bool) {
	switch x := v.(type) {
	case int:
		return x, true
	case int64:
		return int(x), true
	case float64:
		return int(x), true
	case json.Number:
		if n, err := x.Int64(); err == nil {
			return int(n), true
		}
	case string:
		// Best-effort: koanf-style integers can land here.
		var n int
		if _, err := fmt.Sscanf(x, "%d", &n); err == nil {
			return n, true
		}
	}
	return 0, false
}
