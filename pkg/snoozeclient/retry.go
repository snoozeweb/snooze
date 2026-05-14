package snoozeclient

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"syscall"
	"time"

	"github.com/cenkalti/backoff/v4"
)

// isTransient reports whether err looks like a retriable transport-level
// failure: timeouts, connection refused/reset, EOFs, DNS hiccups, etc.
func isTransient(err error) bool {
	if err == nil {
		return false
	}
	// Context cancel/deadline is intentionally NOT transient — the caller
	// asked us to stop.
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		// urlErr.Timeout is implemented for *url.Error.
		if urlErr.Timeout() {
			return true
		}
		// Recurse into the underlying err.
		return isTransient(urlErr.Err)
	}
	if errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.EPIPE) {
		return true
	}
	return false
}

// isRetriableStatus reports whether an HTTP status code merits a retry. Only
// 5xx and 429 (Too Many Requests) are retriable; 4xx and 401-the-first-time
// are caller errors.
func isRetriableStatus(code int) bool {
	if code == http.StatusTooManyRequests {
		return true
	}
	return code >= 500 && code <= 599
}

// newBackoff builds the canonical retry policy: bounded exponential with
// jitter, capped at maxRetries attempts, context-aware.
func newBackoff(ctx context.Context, initial time.Duration, maxRetries int) backoff.BackOffContext {
	exp := backoff.NewExponentialBackOff()
	exp.InitialInterval = initial
	exp.RandomizationFactor = 0.3
	exp.Multiplier = 2.0
	exp.MaxInterval = 5 * time.Second
	// Don't cap on elapsed time — the per-request context already does.
	exp.MaxElapsedTime = 0
	exp.Reset()
	bo := backoff.WithMaxRetries(exp, uint64(maxRetries)) //nolint:gosec // maxRetries is small + non-negative
	return backoff.WithContext(bo, ctx)
}
