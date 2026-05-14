package schema

// Web configures the embedded web UI.
type Web struct {
	Enabled bool   `koanf:"enabled"`
	Path    string `koanf:"path"`
}

// DefaultWeb returns the Python defaults.
func DefaultWeb() Web {
	return Web{
		Enabled: true,
		Path:    "/opt/snooze/web",
	}
}
