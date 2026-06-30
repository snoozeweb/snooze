package middleware

import (
	"context"
	"net"
	"net/http"
)

type peerIPCtxKey struct{}

// CapturePeerIP stashes the genuine TCP peer address (host portion of
// r.RemoteAddr, as the net/http server set it from the socket) into the
// request context. It MUST be mounted BEFORE chi's RealIP middleware, which
// overwrites r.RemoteAddr from the spoofable X-Forwarded-For/X-Real-IP/
// True-Client-IP headers without removing them. After RealIP runs, RemoteAddr
// can no longer be trusted for an allowlist decision; the value captured here
// is the only header-independent peer identity left on the request.
func CapturePeerIP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), peerIPCtxKey{}, hostOnly(r.RemoteAddr))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// PeerIP returns the genuine TCP peer IP captured by CapturePeerIP, with no
// regard to any forwarding header. When CapturePeerIP did not run (e.g. unit
// tests that exercise the bare middleware), it falls back to the host portion
// of the current r.RemoteAddr — still header-independent, because PeerIP never
// consults X-Forwarded-For/X-Real-IP. This is the accessor the auth-proxy trust
// gate uses; ClientIP (XFF-aware) is for audit/ingest only.
func PeerIP(r *http.Request) string {
	if v, ok := r.Context().Value(peerIPCtxKey{}).(string); ok {
		return v
	}
	return hostOnly(r.RemoteAddr)
}

// hostOnly strips the :port from an address, tolerating a bare host, an
// IPv6 [host]:port, and an empty string.
func hostOnly(addr string) string {
	if addr == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return addr // already bare host (no port)
}
