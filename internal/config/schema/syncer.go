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
type Syncer struct {
	Hostname     string   `koanf:"hostname"`
	SyncInterval Duration `koanf:"sync_interval"`
}

// DefaultSyncer returns the canonical defaults; hostname falls back to
// “DefaultHostname“ (OS hostname, then "snooze").
func DefaultSyncer() Syncer {
	return Syncer{
		Hostname:     DefaultHostname(),
		SyncInterval: Duration(time.Second),
	}
}
