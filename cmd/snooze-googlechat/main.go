// Command snooze-googlechat will bridge Google Chat with snooze-server.
// The daemon is a rewrite-in-progress; only the version subcommand works today.
package main

import (
	"fmt"
	"os"

	"github.com/snoozeweb/snooze/internal/daemon"
)

func main() {
	if daemon.HandleVersion("snooze-googlechat", os.Args[1:], os.Stdout) {
		return
	}
	fmt.Fprintln(os.Stderr, "snooze-googlechat: not implemented yet (rewrite in progress)")
	os.Exit(1)
}
