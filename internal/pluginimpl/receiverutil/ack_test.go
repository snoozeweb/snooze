package receiverutil

import "testing"

func TestMapLegacyState_Acknowledged(t *testing.T) {
	if got := MapLegacyState("acknowledged"); got != "ack" {
		t.Fatalf("MapLegacyState(%q) = %q, want %q", "acknowledged", got, "ack")
	}
}

func TestMapLegacyState_AcknowledgedUpperCase(t *testing.T) {
	if got := MapLegacyState("ACKNOWLEDGED"); got != "ack" {
		t.Fatalf("MapLegacyState(%q) = %q, want %q", "ACKNOWLEDGED", got, "ack")
	}
}

func TestMapLegacyState_Closed(t *testing.T) {
	if got := MapLegacyState("closed"); got != "close" {
		t.Fatalf("MapLegacyState(%q) = %q, want %q", "closed", got, "close")
	}
}

func TestMapLegacyState_Resolved(t *testing.T) {
	if got := MapLegacyState("resolved"); got != "close" {
		t.Fatalf("MapLegacyState(%q) = %q, want %q", "resolved", got, "close")
	}
}

func TestMapLegacyState_Open(t *testing.T) {
	if got := MapLegacyState("open"); got != "" {
		t.Fatalf("MapLegacyState(%q) = %q, want %q", "open", got, "")
	}
}

func TestMapLegacyState_Empty(t *testing.T) {
	if got := MapLegacyState(""); got != "" {
		t.Fatalf("MapLegacyState(%q) = %q, want %q", "", got, "")
	}
}

func TestMapLegacyState_UnknownPassthrough(t *testing.T) {
	if got := MapLegacyState("pending"); got != "" {
		t.Fatalf("MapLegacyState(%q) = %q, want %q", "pending", got, "")
	}
}
