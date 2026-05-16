package core

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"golang.org/x/sync/errgroup"

	"github.com/snoozeweb/snooze/internal/config"
	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/internal/telemetry"
)

func TestCore_PluginAccessor(t *testing.T) {
	t.Parallel()
	c := &Core{
		plugins: map[string]plugins.Plugin{
			"rule": &fakeProcessor{name: "rule"},
		},
	}
	require.NotNil(t, c.Plugin("rule"))
	require.Nil(t, c.Plugin("missing"))
}

func TestCore_PluginsSnapshotIsCopy(t *testing.T) {
	t.Parallel()
	c := &Core{
		plugins: map[string]plugins.Plugin{
			"rule": &fakeProcessor{name: "rule"},
		},
	}
	snap := c.Plugins()
	delete(snap, "rule")
	require.NotNil(t, c.Plugin("rule"),
		"mutating the snapshot must not touch the underlying map")
}

func TestCore_HostInterface(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	drv := newFakeDB()
	reg := telemetry.NewRegistry(prometheus.NewRegistry())
	c := &Core{
		Cfg:     cfg,
		Driver:  drv,
		Reg:     reg,
		Trc:     otel.Tracer("test"),
		Loggers: &telemetry.Loggers{Snooze: slog.New(slog.NewTextHandler(io.Discard, nil))},
	}
	// Compile-time check is already in host.go; verify the methods route to
	// the right struct fields at run-time.
	require.Same(t, drv, c.DB())
	require.Same(t, cfg, c.Config())
	require.NotNil(t, c.Tracer())
	require.Same(t, reg, c.Metrics())
	require.NotNil(t, c.Logger())
	// Driver.Watcher returns nil → Host.Bus must return nil.
	require.Nil(t, c.Bus())
}

func TestCore_RunHonoursContext(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	c := &Core{
		Cfg:     cfg,
		Driver:  newFakeDB(),
		Reg:     telemetry.NewRegistry(prometheus.NewRegistry()),
		Trc:     otel.Tracer("test"),
		Loggers: &telemetry.Loggers{Snooze: slog.New(slog.NewTextHandler(io.Discard, nil))},
	}
	c.Sup = &Supervisor{Logger: c.Logger(), Metrics: c.Reg}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	// No subsystems wired — Run must still respect ctx cancellation via errgroup.
	require.NoError(t, c.Run(ctx))
}

// Sanity check that the Job and supervisor can be wired through errgroup
// without referencing un-exported helpers.
func TestCore_SupervisorWiring(t *testing.T) {
	t.Parallel()
	sup := &Supervisor{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	g, gctx := errgroup.WithContext(context.Background())
	done := make(chan struct{})
	sup.Go(gctx, g, Job{
		Name: "wire",
		Fn: func(_ context.Context) error {
			close(done)
			return nil
		},
	})
	require.NoError(t, g.Wait())
	<-done
}
