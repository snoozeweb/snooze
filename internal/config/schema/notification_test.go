package schema

import "testing"

func TestDefaultNotificationPersistActionOutcomes(t *testing.T) {
	got := DefaultNotification()
	if !got.PersistActionOutcomes {
		t.Fatalf("PersistActionOutcomes default = false, want true")
	}
}
