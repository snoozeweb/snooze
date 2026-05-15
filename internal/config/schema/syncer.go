package schema

import (
	"os"
	"time"
)

// Syncer configures the cluster-syncer thread.
type Syncer struct {
	Hostname       string   `koanf:"hostname"`
	Total          int      `koanf:"total" validate:"min=1"`
	SyncInterval   Duration `koanf:"sync_interval"`
	SyncIntervalMS int      `koanf:"sync_interval_ms"`
}

// DefaultSyncer returns the Python defaults; hostname falls back to
// “os.Hostname“ exactly like Python's “socket.gethostname“ shim.
func DefaultSyncer() Syncer {
	host, err := os.Hostname()
	if err != nil {
		host = "snooze"
	}
	return Syncer{
		Hostname:       host,
		Total:          1,
		SyncInterval:   Duration(time.Second),
		SyncIntervalMS: 1000,
	}
}
