package schema

import "time"

// Housekeeper carries the periodic-cleanup tunables. Durations are stored as
// :type:`Duration` so they accept both Go-style strings and the bare seconds
// emitted by the legacy Python YAML.
type Housekeeper struct {
	TriggerOnStartup    bool     `koanf:"trigger_on_startup"`
	RecordTTL           Duration `koanf:"record_ttl"`
	CleanupAlert        Duration `koanf:"cleanup_alert"`
	CleanupAggregate    Duration `koanf:"cleanup_aggregate"`
	CleanupComment      Duration `koanf:"cleanup_comment"`
	CleanupOrphans      Duration `koanf:"cleanup_orphans"`
	CleanupAudit        Duration `koanf:"cleanup_audit"`
	CleanupSnooze       Duration `koanf:"cleanup_snooze"`
	CleanupNotification Duration `koanf:"cleanup_notification"`
	RenumberField       Duration `koanf:"renumber_field"`
}

// DefaultHousekeeper returns the Python defaults.
func DefaultHousekeeper() Housekeeper {
	return Housekeeper{
		TriggerOnStartup:    true,
		RecordTTL:           Duration(2 * 24 * time.Hour),
		CleanupAlert:        Duration(5 * time.Minute),
		CleanupAggregate:    Duration(5 * time.Minute),
		CleanupComment:      Duration(24 * time.Hour),
		CleanupOrphans:      Duration(24 * time.Hour),
		CleanupAudit:        Duration(28 * 24 * time.Hour),
		CleanupSnooze:       Duration(3 * 24 * time.Hour),
		CleanupNotification: Duration(3 * 24 * time.Hour),
		RenumberField:       Duration(24 * time.Hour),
	}
}
