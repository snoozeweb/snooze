// Command snooze is the Snooze CLI client. It is a thin wrapper around
// internal/cli.Execute, which builds the cobra command tree and dispatches
// the subcommand. The split keeps the command graph unit-testable without
// pulling in the main entry point.
package main

import (
	"os"

	"github.com/snoozeweb/snooze/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
