package version

// Injected at build time via -ldflags "-X ...". Defaults are dev placeholders.
var (
	Version = "0.0.0-dev"
	Commit  = "none"
	Date    = "unknown"
)

func String() string { return Version + " (" + Commit + ", " + Date + ")" }
