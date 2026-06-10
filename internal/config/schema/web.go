package schema

// Web configures the embedded web UI.
type Web struct {
	Enabled bool   `koanf:"enabled"`
	Path    string `koanf:"path"`
}

// DefaultWeb returns the Go packaging defaults. The deb/rpm install the React
// bundle in /var/lib/snooze/web (the Python 1.x location was /opt/snooze/web —
// migrated configs carrying that value point at the obsolete Python UI and
// should drop or update the field).
func DefaultWeb() Web {
	return Web{
		Enabled: true,
		Path:    "/var/lib/snooze/web",
	}
}
