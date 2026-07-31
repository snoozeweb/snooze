package schema

import (
	"os"
	"time"
)

// DefaultHostname is the single source of truth for the node identity used by
// the syncer/heartbeat when nothing better is configured. It mirrors the
// behaviour of Python's “socket.gethostname“ shim: the OS hostname when
// available, falling back to a stable literal otherwise. The heartbeat runner
// reuses this so the config layer and the runtime never disagree on the
// fallback name.
func DefaultHostname() string {
	if host, err := os.Hostname(); err == nil && host != "" {
		return host
	}
	return "snooze"
}

// Syncer configures the cluster-syncer thread.
//
// SyncInterval is the single source of truth for the heartbeat/debounce
// cadence. The legacy “sync_interval_ms“ knob was removed: it duplicated this
// Duration and was never consumed at runtime.
// ReloadSafetyInterval is the cadence of the periodic full plugin reload that
// backstops event delivery. Change events are the fast path; this bounds how
// long a lost or misrouted one can leave an in-memory cache stale (the failure
// mode where an edited filter only takes effect after a restart). A negative
// value disables the backstop; zero selects the syncer's own default.
type Syncer struct {
	Hostname             string   `koanf:"hostname"`
	SyncInterval         Duration `koanf:"sync_interval"`
	ReloadSafetyInterval Duration `koanf:"reload_safety_interval"`
}

// DefaultSyncer returns the canonical defaults; hostname falls back to
// “DefaultHostname“ (OS hostname, then "snooze").
func DefaultSyncer() Syncer {
	return Syncer{
		Hostname:             DefaultHostname(),
		SyncInterval:         Duration(time.Second),
		ReloadSafetyInterval: Duration(5 * time.Minute),
	}
}
