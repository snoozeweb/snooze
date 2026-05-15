package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/japannext/snooze/internal/condition"
	"github.com/japannext/snooze/internal/config/schema"
	"github.com/japannext/snooze/internal/db"
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
func (NoopRuntimeStore) Watch(ctx context.Context, _ string) (<-chan RuntimeChange, error) {
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

// settingsCollection is the DB collection holding the flat key/value
// catalogue rows (“{uid, name, value, comment}“) the React Settings page
// writes to.
const settingsCollection = "settings"

// RuntimeSettings reads DB-backed settings with a small read-through cache.
// The PATCH/POST/PUT/DELETE handler for the “settings“ collection calls
// “Invalidate“ so an edit in the UI takes effect on the next read.
//
// Layered defaulting: the bootstrap Config provides the baseline; values
// stored in the DB override per-key. Keys use the dotted-prefix
// convention (“ldap.host“, “housekeeping.cleanup_snooze“).
type RuntimeSettings struct {
	drv      db.Driver
	baseline *Config
	cacheTTL time.Duration

	mu        sync.RWMutex
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
		drv:      drv,
		baseline: baseline,
		cacheTTL: cacheTTL,
	}
}

// Invalidate forces the next read to refresh from the DB. Safe to call
// concurrently with any other RuntimeSettings method.
func (r *RuntimeSettings) Invalidate() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.cached = nil
	r.expiresAt = time.Time{}
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

// load fetches the cached catalogue or refreshes it from the DB. We hold an
// RLock for the cache-hit fast path and upgrade to a Lock only when the
// cache has expired.
func (r *RuntimeSettings) load(ctx context.Context) (map[string]any, error) {
	r.mu.RLock()
	if r.cached != nil && time.Now().Before(r.expiresAt) {
		out := r.cached
		r.mu.RUnlock()
		return out, nil
	}
	r.mu.RUnlock()

	r.mu.Lock()
	defer r.mu.Unlock()
	// Re-check under the write lock — another goroutine may have refreshed.
	if r.cached != nil && time.Now().Before(r.expiresAt) {
		return r.cached, nil
	}
	values, err := r.readAll(ctx)
	if err != nil {
		return nil, err
	}
	r.cached = values
	r.cachedAt = time.Now()
	r.expiresAt = r.cachedAt.Add(r.cacheTTL)
	return values, nil
}

// readAll scans every row in the settings collection and folds it into a
// flat map keyed by `name`. Missing collection / empty collection both
// return an empty map.
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
	overlayDuration(values, "housekeeping.cleanup_comment", &out.CleanupComment)
	overlayDuration(values, "housekeeping.cleanup_snooze", &out.CleanupSnooze)
	overlayDuration(values, "housekeeping.cleanup_notification", &out.CleanupNotification)
	overlayDuration(values, "housekeeping.cleanup_audit", &out.CleanupAudit)
	overlayDuration(values, "housekeeping.cleanup_orphans", &out.CleanupOrphans)
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
