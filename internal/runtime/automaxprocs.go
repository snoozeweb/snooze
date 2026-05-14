// Package runtime provides side-effect imports that every Snooze binary needs
// to behave correctly inside a container.
//
// The blank import of go.uber.org/automaxprocs adjusts GOMAXPROCS at startup
// to match the cgroup CPU quota when the binary is running inside a container.
// Without it the Go runtime defaults GOMAXPROCS to the number of host CPUs,
// which on a busy node with a low CPU limit produces excessive scheduler
// contention and steady tail-latency regressions.
//
// Every cmd/*/main.go blank-imports this package so the side effect runs in
// every binary without each command having to remember the dependency.
package runtime

import (
	// Side-effect import: at package init, automaxprocs calls
	// runtime.GOMAXPROCS(N) where N is derived from the cgroup CPU quota.
	_ "go.uber.org/automaxprocs"
)
