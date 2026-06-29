package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestClientIP_XForwardedFor verifies the X-Forwarded-For branch wins and the
// first (left-most) hop is returned, trimmed of surrounding whitespace.
func TestClientIP_XForwardedFor(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "192.0.2.1:5555"
	req.Header.Set("X-Real-IP", "198.51.100.7")
	req.Header.Set("X-Forwarded-For", "203.0.113.5, 70.41.3.18, 150.172.238.178")

	if got := ClientIP(req); got != "203.0.113.5" {
		t.Fatalf("ClientIP = %q, want %q", got, "203.0.113.5")
	}
}

// TestClientIP_XRealIP verifies the X-Real-IP branch is used when no
// X-Forwarded-For header is present.
func TestClientIP_XRealIP(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "192.0.2.1:5555"
	req.Header.Set("X-Real-IP", "198.51.100.7")

	if got := ClientIP(req); got != "198.51.100.7" {
		t.Fatalf("ClientIP = %q, want %q", got, "198.51.100.7")
	}
}

// TestClientIP_RemoteAddr verifies the RemoteAddr fallback strips the port.
func TestClientIP_RemoteAddr(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "192.0.2.1:5555"

	if got := ClientIP(req); got != "192.0.2.1" {
		t.Fatalf("ClientIP = %q, want %q", got, "192.0.2.1")
	}
}

// TestClientIP_Empty verifies an empty string is returned when no source of an
// IP is available.
func TestClientIP_Empty(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = ""

	if got := ClientIP(req); got != "" {
		t.Fatalf("ClientIP = %q, want empty", got)
	}
}
