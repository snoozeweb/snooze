// Command snooze-snmptrap receives SNMP traps and forwards to snooze-server.
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

	"github.com/japannext/snooze/internal/components/snmptrap"
	"github.com/japannext/snooze/internal/version"

	// Blank-imported so GOMAXPROCS auto-tunes to the container CPU quota.
	_ "github.com/japannext/snooze/internal/runtime"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Println("snooze-snmptrap", version.String())
		return
	}

	var cfgPath string
	fs := flag.NewFlagSet("snooze-snmptrap", flag.ExitOnError)
	fs.StringVar(&cfgPath, "c", "/etc/snooze/snmptrap.yaml", "path to the snmptrap YAML config")
	if err := fs.Parse(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "snooze-snmptrap:", err)
		os.Exit(2)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := snmptrap.LoadConfig(cfgPath)
	if err != nil {
		logger.Error("snooze-snmptrap: config load", slog.Any("err", err))
		os.Exit(1)
	}

	daemon, err := snmptrap.NewDaemon(cfg, logger)
	if err != nil {
		logger.Error("snooze-snmptrap: build daemon", slog.Any("err", err))
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger.Info("snooze-snmptrap: starting",
		slog.String("version", version.String()),
		slog.String("listen", cfg.Listen),
		slog.String("server", cfg.Server),
	)

	if err := daemon.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		logger.Error("snooze-snmptrap: exited with error", slog.Any("err", err))
		os.Exit(1)
	}
	logger.Info("snooze-snmptrap: shutdown complete")
}
