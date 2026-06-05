// Package snoozetypes — tenant constants.
package snoozetypes

// DefaultTenant is the reserved, immutable tenant created at first boot. It is
// the fallback for tokenless ingest (D4) and the default login org (D10).
// It cannot be deleted or renamed.
const DefaultTenant = "default"
