package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	chimw "github.com/go-chi/chi/v5/middleware"
)

// TestPeerIP_IgnoresForwardingHeaders proves PeerIP returns the genuine TCP
// peer (the host portion of the original RemoteAddr) and never consults any
// forwarding header — even after chi's RealIP has rewritten RemoteAddr from
// those very headers, because CapturePeerIP ran first.
func TestPeerIP_IgnoresForwardingHeaders(t *testing.T) {
	// Bare CapturePeerIP (no RealIP): the headers are present but ignored.
	var seen string
	h := CapturePeerIP(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = PeerIP(r)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.7:5555"
	req.Header.Set("X-Forwarded-For", "10.0.0.1")
	req.Header.Set("X-Real-IP", "10.0.0.2")
	req.Header.Set("True-Client-IP", "10.0.0.3")
	h.ServeHTTP(httptest.NewRecorder(), req)
	if seen != "203.0.113.7" {
		t.Fatalf("PeerIP = %q, want %q", seen, "203.0.113.7")
	}

	// Now prove header-independence under RealIP: capture first, THEN RealIP.
	// RealIP rewrites RemoteAddr to 10.0.0.3 (True-Client-IP wins) and
	// ClientIP(r) returns 10.0.0.1 (X-Forwarded-For), but PeerIP must still
	// be the original socket peer captured before RealIP ran.
	var peerUnderRealIP, clientUnderRealIP, remoteAddrUnderRealIP string
	chain := CapturePeerIP(chimw.RealIP(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		peerUnderRealIP = PeerIP(r)
		clientUnderRealIP = ClientIP(r)
		remoteAddrUnderRealIP = r.RemoteAddr
	})))
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.RemoteAddr = "203.0.113.7:5555"
	req2.Header.Set("X-Forwarded-For", "10.0.0.1")
	req2.Header.Set("X-Real-IP", "10.0.0.2")
	req2.Header.Set("True-Client-IP", "10.0.0.3")
	chain.ServeHTTP(httptest.NewRecorder(), req2)
	if peerUnderRealIP != "203.0.113.7" {
		t.Fatalf("PeerIP under RealIP = %q, want %q (must ignore forwarding headers)", peerUnderRealIP, "203.0.113.7")
	}
	// Sanity: RealIP did rewrite RemoteAddr and ClientIP does honor the headers,
	// proving the divergence the fix relies on.
	if clientUnderRealIP != "10.0.0.1" {
		t.Fatalf("ClientIP under RealIP = %q, want %q (XFF-first)", clientUnderRealIP, "10.0.0.1")
	}
	if remoteAddrUnderRealIP == "203.0.113.7:5555" {
		t.Fatalf("RealIP did not rewrite RemoteAddr (=%q); test assumption broken", remoteAddrUnderRealIP)
	}
}

// TestPeerIP_IPv6 verifies the host-only extraction handles a bracketed IPv6
// RemoteAddr (where ClientIP's naive last-colon trim would have mangled it).
func TestPeerIP_IPv6(t *testing.T) {
	var seen string
	h := CapturePeerIP(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = PeerIP(r)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "[2001:db8::1]:443"
	h.ServeHTTP(httptest.NewRecorder(), req)
	if seen != "2001:db8::1" {
		t.Fatalf("PeerIP = %q, want %q", seen, "2001:db8::1")
	}
}

// TestPeerIP_FallbackNoCapture verifies that when CapturePeerIP did NOT run,
// PeerIP falls back to the host portion of the current RemoteAddr (still
// header-independent — PeerIP never reads forwarding headers).
func TestPeerIP_FallbackNoCapture(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "192.0.2.9:1234"
	req.Header.Set("X-Forwarded-For", "10.0.0.1")
	if got := PeerIP(req); got != "192.0.2.9" {
		t.Fatalf("PeerIP (no capture) = %q, want %q", got, "192.0.2.9")
	}
}
