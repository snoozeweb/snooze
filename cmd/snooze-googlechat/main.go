// Command snooze-googlechat bridges Google Chat with snooze-server.
package main

import (
	"fmt"
	"os"

	"github.com/japannext/snooze/internal/version"

	// Blank-imported so GOMAXPROCS auto-tunes to the container CPU quota.
	_ "github.com/japannext/snooze/internal/runtime"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Println("snooze-googlechat", version.String())
		return
	}
	fmt.Fprintln(os.Stderr, "snooze-googlechat: not implemented yet (rewrite in progress)")
	os.Exit(1)
}
