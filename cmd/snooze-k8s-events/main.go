// Command snooze-k8s-events watches the Kubernetes core/v1 Event API and
// forwards interesting events (Warnings by default) to snooze-server as alerts.
//
// It talks to the kube-apiserver over plain HTTP (no k8s.io/client-go). When
// run inside a cluster with no explicit config it auto-detects the projected
// ServiceAccount token, CA and apiserver address; otherwise it uses the
// apiserver/token/ca_cert from k8s-events.yaml.
//
// Usage:
//
//	snooze-k8s-events              # use /etc/snooze/k8s-events.yaml
//	snooze-k8s-events -c path.yaml # explicit config
//	snooze-k8s-events version      # print build info and exit
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

	"github.com/snoozeweb/snooze/internal/components/k8sevents"
	"github.com/snoozeweb/snooze/internal/version"

	// Blank-imported so GOMAXPROCS auto-tunes to the container CPU quota.
	_ "github.com/snoozeweb/snooze/internal/runtime"
)

// defaultConfigPath is the systemd-friendly location packaging installs to.
const defaultConfigPath = "/etc/snooze/k8s-events.yaml"

func run() int {
	cfgPath := flag.String("c", defaultConfigPath, "path to k8s-events.yaml")
	debug := flag.Bool("debug", false, "enable debug logging")
	flag.Parse()

	cfg, err := k8sevents.LoadConfig(*cfgPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "snooze-k8s-events:", err)
		return 2
	}

	level := slog.LevelInfo
	if *debug || cfg.Debug {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)

	d, err := k8sevents.New(cfg, logger)
	if err != nil {
		fmt.Fprintln(os.Stderr, "snooze-k8s-events:", err)
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := d.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, "snooze-k8s-events:", err)
		return 1
	}
	return 0
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Println("snooze-k8s-events", version.String())
		return
	}
	os.Exit(run())
}
