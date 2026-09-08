package schema

import "testing"

func TestDefaultNotificationPersistActionOutcomes(t *testing.T) {
	got := DefaultNotification()
	if !got.PersistActionOutcomes {
		t.Fatalf("PersistActionOutcomes default = false, want true")
	}
}

// TestDefaultNotificationDeliveryLog pins the delivery-history default: the
// dispatcher writes a notificationlog row per send unless an operator opts out.
func TestDefaultNotificationDeliveryLog(t *testing.T) {
	got := DefaultNotification()
	if !got.DeliveryLog {
		t.Fatalf("DeliveryLog default = false, want true")
	}
}
