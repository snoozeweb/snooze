package schema

import "time"

// Notification holds the default retry/frequency tunables for the
// notification dispatcher.
type Notification struct {
	NotificationFreq  Duration `koanf:"notification_freq"`
	NotificationRetry int      `koanf:"notification_retry" validate:"min=0"`
	// PersistActionOutcomes gates the asynchronous write-back of per-action
	// send results (pending → success/error) onto the alert record. When
	// false, sent actions are recorded as `sent` and no resolution write is
	// issued — useful on single-writer SQLite at high alert volume. The
	// matched-notifications list and the static action classification
	// (error/skipped) are stamped regardless, in the existing pipeline write.
	PersistActionOutcomes bool `koanf:"persist_action_outcomes"`
	// DeliveryLog gates the per-send delivery history: when true (the
	// default) every send attempt the dispatcher performs writes one row into
	// the tenant-scoped `notificationlog` collection — action, notifier,
	// status, duration, the notification(s) it fired for and a snapshot of the
	// alert(s) it covered. Turn it off on write-constrained deployments
	// (single-writer SQLite at high alert volume): the per-action outcome
	// stamps on the record and the notification hit counters are unaffected,
	// only the history rows stop being written. Runtime-overridable from
	// Settings -> Notifications via the `notification.delivery_log` key.
	// Retention of the rows is `housekeeping.cleanup_notificationlog`.
	DeliveryLog bool `koanf:"delivery_log"`
}

// DefaultNotification returns the Python defaults.
func DefaultNotification() Notification {
	return Notification{
		NotificationFreq:      Duration(time.Minute),
		NotificationRetry:     3,
		PersistActionOutcomes: true,
		DeliveryLog:           true,
	}
}
