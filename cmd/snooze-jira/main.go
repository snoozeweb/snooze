// Command snooze-jira is the JIRA Cloud output bridge.
//
// The daemon exposes a small HTTP server on /alert that snooze-server hits as
// a webhook action. On the first alert for a record it creates a JIRA issue;
// on re-escalations it adds a comment to the existing ticket (and optionally
// reopens it). An optional background poller closes Snooze records when the
// corresponding JIRA ticket transitions to Done.
//
// Usage:
//
//	snooze-jira              # use /etc/snooze/jira.yaml
//	snooze-jira -c path.yaml # explicit config
//	snooze-jira version      # print build info and exit
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/japannext/snooze/internal/components/jira"
	"github.com/japannext/snooze/internal/version"

	// Blank-imported so GOMAXPROCS auto-tunes to the container CPU quota.
	_ "github.com/japannext/snooze/internal/runtime"
)

// defaultConfigPath is the systemd-friendly location packaging will install
// to. The -c flag overrides it.
const defaultConfigPath = "/etc/snooze/jira.yaml"

func run() int {
	cfgPath := flag.String("c", defaultConfigPath, "path to jira.yaml")
	debug := flag.Bool("debug", false, "enable debug logging")
	flag.Parse()

	cfg, err := jira.LoadConfig(*cfgPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "snooze-jira:", err)
		return 2
	}

	level := slog.LevelInfo
	if *debug || cfg.Debug {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)

	d, err := jira.New(cfg, logger)
	if err != nil {
		fmt.Fprintln(os.Stderr, "snooze-jira:", err)
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := d.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, "snooze-jira:", err)
		return 1
	}
	return 0
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Println("snooze-jira", version.String())
		return
	}
	os.Exit(run())
}
