// Command snooze is the Snooze CLI client. It is a thin wrapper around
// internal/cli.Execute, which builds the cobra command tree and dispatches
// the subcommand. The split keeps the command graph unit-testable without
// pulling in the main entry point.
package main

import (
	"os"

	"github.com/snoozeweb/snooze/internal/cli"

	// Blank-imported so GOMAXPROCS auto-tunes to the container CPU quota.
	_ "github.com/snoozeweb/snooze/internal/runtime"
)

func main() {
	os.Exit(cli.Execute())
}
