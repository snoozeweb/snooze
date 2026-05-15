// Package version provides build-time version metadata.
package version

// Injected at build time via -ldflags "-X ...". Defaults are dev placeholders.
var (
	Version = "0.0.0-dev"
	Commit  = "none"
	Date    = "unknown"
)

// String returns a human-readable version string including commit and date.
func String() string { return Version + " (" + Commit + ", " + Date + ")" }
