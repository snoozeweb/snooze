package schema

import "time"

// DefaultShelveTimeout is the fallback timed-shelve window used when neither the
// runtime settings nor the file-config baseline supply one. A time-boxed shelve
// lasts this long before the unshelve-timeout sweep returns the alert to open.
const DefaultShelveTimeout = 4 * time.Hour

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
	CleanupStats        Duration `koanf:"cleanup_stats"`
	CleanupSnooze       Duration `koanf:"cleanup_snooze"`
	CleanupNotification Duration `koanf:"cleanup_notification"`
	CleanupAPIKey       Duration `koanf:"cleanup_apikey"`
	CleanupRefreshToken Duration `koanf:"cleanup_refresh_token"`
	// AckTimeout is how long an acknowledgement holds before the
	// escalate-timeout sweep reverts the record from "ack" to "open". Default
	// 24h (Alerta's ACK_TIMEOUT is 7200s; we pick a more on-call-friendly day).
	AckTimeout Duration `koanf:"ack_timeout"`
	// EscalateAfter is how long an un-acknowledged open alert may sit before the
	// sweep auto-escalates it to "esc" and re-fires its notifications. Default 0
	// disables auto-escalation entirely (the escalate pass is a no-op).
	EscalateAfter Duration `koanf:"escalate_after"`
	// ShelveTimeout is how long a time-boxed shelve lasts before the
	// unshelve-timeout sweep reverts the alert from "shelved" to "open". Stamped
	// onto shelve_until when a shelve comment is posted. Default 4h.
	ShelveTimeout Duration `koanf:"shelve_timeout"`
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
		CleanupStats:        Duration(400 * 24 * time.Hour),
		CleanupSnooze:       Duration(3 * 24 * time.Hour),
		CleanupNotification: Duration(3 * 24 * time.Hour),
		CleanupAPIKey:       Duration(time.Hour),
		CleanupRefreshToken: Duration(time.Hour),
		AckTimeout:          Duration(24 * time.Hour),
		EscalateAfter:       Duration(0),
		ShelveTimeout:       Duration(DefaultShelveTimeout),
	}
}
