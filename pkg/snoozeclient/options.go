// Package snoozeclient is the official Go client for the Snooze v1 REST API.
//
// It encapsulates the v1 conventions used by the rest of the codebase:
//
//   - All endpoints live under /api/v1.
//   - Authentication is bearer: Authorization: Bearer <token>.
//   - The error envelope is {"error":{"code","message","details","request_id","trace_id"}}.
//
// Typical usage:
//
//	c, err := snoozeclient.New(snoozeclient.Options{
//	    BaseURL:  "https://snooze.example.com",
//	    Username: "alice",
//	    Password: os.Getenv("SNOOZE_PASSWORD"),
//	})
//	if err != nil { ... }
//	if err := c.Login(ctx); err != nil { ... }
//	rec, err := c.PostAlert(ctx, snoozetypes.Record{...})
//
// The client is safe for concurrent use across goroutines.
package snoozeclient

import (
	"crypto/tls"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DefaultTimeout is the request timeout applied when Options.Timeout is zero.
const DefaultTimeout = 30 * time.Second

// DefaultMaxRetries is the maximum number of retry attempts (in addition to
// the initial call) applied when Options.MaxRetries is zero.
const DefaultMaxRetries = 3

// DefaultInitialBackoff is the initial backoff interval between retries when
// Options.InitialBackoff is zero.
const DefaultInitialBackoff = 200 * time.Millisecond

// Options bundles all knobs accepted by New. Fields with sensible zero values
// are documented inline; required fields are called out explicitly.
type Options struct {
	// BaseURL is the Snooze server origin (scheme + host[:port]), e.g.
	// "https://snooze.example.com". Required. A trailing slash is tolerated.
	BaseURL string

	// Username / Password are the credentials used by Login. Required for the
	// first login unless Token is supplied. Username is also used by the
	// cache file to namespace tokens when multiple users share a homedir.
	Username string
	Password string

	// Method selects the auth backend on the server side. Defaults to "local".
	// Other accepted values are "ldap" and "anonymous".
	Method string

	// Token, when non-empty, is used as the bearer token without contacting
	// the /login endpoint. The token cache is still updated on Login so a
	// later session can reuse the value.
	Token string

	// IngestToken, when non-empty, is sent as `Authorization: Bearer <IngestToken>`
	// on every outbound request instead of the Login-obtained bearer token. Use
	// this to authenticate daemons against a specific Snooze tenant's ingest token
	// (D4) without going through the user login flow.
	IngestToken string

	// Timeout is applied to every HTTP request. Defaults to DefaultTimeout.
	Timeout time.Duration

	// Insecure disables TLS certificate verification. Off by default; flip
	// only for local development against self-signed certs.
	Insecure bool

	// TokenCacheFile overrides the default cache location. Empty means
	// $XDG_CACHE_HOME/snooze/token (with a $HOME/.snooze-token fallback when
	// neither $XDG_CACHE_HOME nor os.UserCacheDir() are usable).
	TokenCacheFile string

	// MaxRetries caps the number of retries (beyond the initial attempt) for
	// transient failures. Defaults to DefaultMaxRetries.
	MaxRetries int

	// InitialBackoff seeds the exponential backoff. Defaults to
	// DefaultInitialBackoff.
	InitialBackoff time.Duration

	// HTTPClient lets callers inject a fully-formed *http.Client; useful for
	// tests that want to wire a custom transport. When nil, the client is
	// built from Timeout / Insecure.
	HTTPClient *http.Client

	// Logger receives debug/info events. Defaults to slog.Default().
	Logger *slog.Logger
}

// ErrMissingBaseURL is returned when Options.BaseURL is empty.
var ErrMissingBaseURL = errors.New("snoozeclient: BaseURL is required")

// resolve fills in defaults and returns a normalised Options copy plus the
// resolved cache-file path. Invalid combinations (missing BaseURL) return
// an error.
func (o Options) resolve() (Options, string, error) {
	out := o
	if strings.TrimSpace(out.BaseURL) == "" {
		return out, "", ErrMissingBaseURL
	}
	out.BaseURL = strings.TrimRight(out.BaseURL, "/")
	if out.Method == "" {
		out.Method = "local"
	}
	if out.Timeout <= 0 {
		out.Timeout = DefaultTimeout
	}
	if out.MaxRetries <= 0 {
		out.MaxRetries = DefaultMaxRetries
	}
	if out.InitialBackoff <= 0 {
		out.InitialBackoff = DefaultInitialBackoff
	}
	if out.Logger == nil {
		out.Logger = slog.Default()
	}
	cache, err := resolveCachePath(out.TokenCacheFile)
	if err != nil {
		return out, "", err
	}
	out.TokenCacheFile = cache
	return out, cache, nil
}

// buildHTTPClient returns a configured *http.Client. When Options.HTTPClient
// is set it is returned as-is (caller-controlled).
func (o Options) buildHTTPClient() *http.Client {
	if o.HTTPClient != nil {
		return o.HTTPClient
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if o.Insecure {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // opt-in
	}
	return &http.Client{Timeout: o.Timeout, Transport: tr}
}

// resolveCachePath picks the on-disk location for the token cache. Order:
//  1. Explicit override (returned verbatim — caller's responsibility).
//  2. os.UserCacheDir() + "/snooze/token".
//  3. $HOME/.snooze-token.
//  4. /tmp/.snooze-token as a last-resort fallback.
//
// Candidates 2 and 3 are *probed* by ensuring the parent directory exists and
// is writable. The earlier version returned the os.UserCacheDir() path even
// when its parent ($HOME/.cache) wasn't writable — which is the common case
// for systemd-launched daemons whose user has no home directory. The
// resulting writeTokenFile mkdir then failed at startup and surfaced a noisy
// WARN. Probing here returns a path the caller can actually write to and
// keeps the tmp fallback silent.
func resolveCachePath(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	if dir, err := os.UserCacheDir(); err == nil && dir != "" {
		candidate := filepath.Join(dir, "snooze", "token")
		if canWriteDir(filepath.Dir(candidate)) {
			return candidate, nil
		}
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		candidate := filepath.Join(home, ".snooze-token")
		if canWriteDir(filepath.Dir(candidate)) {
			return candidate, nil
		}
	}
	// /tmp is world-writable on every POSIX system we deploy on; treat it as
	// the no-error fallback so daemons without a home dir don't trigger the
	// "cache fallback" warning in client.New on every restart.
	return filepath.Join(os.TempDir(), ".snooze-token"), nil
}

// canWriteDir returns true when dir already exists and is writable, OR can
// be MkdirAll-ed and the caller can write to it. Used by resolveCachePath
// to verify a candidate before handing it back to the caller. Any error
// (permission denied, ENOSPC, parent missing on a read-only volume) yields
// false so the next candidate gets a shot.
func canWriteDir(dir string) bool {
	if dir == "" {
		return false
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return false
	}
	// MkdirAll succeeded — confirm writability via a probe file rather than
	// trusting permissions alone (NFS / overlayfs can mkdir-but-not-write).
	f, err := os.CreateTemp(dir, ".snooze-probe-*")
	if err != nil {
		return false
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return true
}
