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
}

// DefaultNotification returns the Python defaults.
func DefaultNotification() Notification {
	return Notification{
		NotificationFreq:      Duration(time.Minute),
		NotificationRetry:     3,
		PersistActionOutcomes: true,
	}
}
