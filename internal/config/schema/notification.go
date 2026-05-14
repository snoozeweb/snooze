package schema

import "time"

// Notification holds the default retry/frequency tunables for the
// notification dispatcher.
type Notification struct {
	NotificationFreq  Duration `koanf:"notification_freq"`
	NotificationRetry int      `koanf:"notification_retry" validate:"min=0"`
}

// DefaultNotification returns the Python defaults.
func DefaultNotification() Notification {
	return Notification{
		NotificationFreq:  Duration(time.Minute),
		NotificationRetry: 3,
	}
}
