// Command snooze-syslog ingests syslog (RFC3164/5424) over UDP and/or TCP and
// forwards each parsed message to a Snooze server's /api/v1/alerts endpoint.
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

	"github.com/japannext/snooze/internal/components/syslog"
	"github.com/japannext/snooze/internal/version"

	// Blank-imported so GOMAXPROCS auto-tunes to the container CPU quota.
	_ "github.com/japannext/snooze/internal/runtime"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Println("snooze-syslog", version.String())
		return
	}

	fs := flag.NewFlagSet("snooze-syslog", flag.ExitOnError)
	cfgPath := fs.String("c", "/etc/snooze/syslog.yaml", "path to the syslog daemon config file")
	if err := fs.Parse(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "snooze-syslog:", err)
		os.Exit(2)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := syslog.LoadConfig(*cfgPath)
	if err != nil {
		logger.Error("config error", slog.Any("err", err))
		os.Exit(1)
	}

	daemon, err := syslog.New(cfg, logger)
	if err != nil {
		logger.Error("daemon init failed", slog.Any("err", err))
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := daemon.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		logger.Error("daemon exited", slog.Any("err", err))
		os.Exit(1)
	}
}
