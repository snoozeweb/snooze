// Command snooze-server is the Snooze monitoring server daemon.
//
// The binary owns the bring-up sequence: load the bootstrap YAML config,
// initialise telemetry, open a database driver, construct the Core
// orchestrator, build the plugin set, bootstrap the root user, then start
// the HTTP listener and the admin Unix socket. Subsystems run under a single
// errgroup; SIGINT/SIGTERM cancels the root context and triggers a graceful
// shutdown.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/snoozeweb/snooze/internal/api"
	"github.com/snoozeweb/snooze/internal/api/middleware"
	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/config"
	"github.com/snoozeweb/snooze/internal/config/schema"
	"github.com/snoozeweb/snooze/internal/core"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/db/mongo"
	"github.com/snoozeweb/snooze/internal/db/postgres"
	"github.com/snoozeweb/snooze/internal/db/sqlite"
	"github.com/snoozeweb/snooze/internal/telemetry"
	"github.com/snoozeweb/snooze/internal/version"

	// Blank-imported to trigger every plugin package's init() and populate
	// the process-wide plugin registry. Only the server binary needs this.
	_ "github.com/snoozeweb/snooze/internal/pluginimpl/all"

	// Blank-imported so GOMAXPROCS auto-tunes to the container CPU quota.
	_ "go.uber.org/automaxprocs"

	"github.com/prometheus/client_golang/prometheus"
)

// coreAdapter exposes *core.Core under the method name api.AlertProcessor
// expects (ProcessRecord). Core already implements the loose-map pipeline
// adapter as ProcessRecordMap; we rename the entry point here to avoid a
// signature collision with the typed Core.ProcessRecord that takes a
// snoozetypes.Record.
type coreAdapter struct{ *core.Core }

// ProcessRecord forwards to Core.ProcessRecordMap. The signature matches
// api.AlertProcessor.
func (a *coreAdapter) ProcessRecord(ctx context.Context, rec map[string]any) (map[string]any, error) {
	return a.ProcessRecordMap(ctx, rec)
}

// Compile-time guarantee that the adapter satisfies api.AlertProcessor.
var _ api.AlertProcessor = (*coreAdapter)(nil)

// exitCodes used by main + helpers so tests can assert behaviour.
const (
	exitOK    = 0
	exitUsage = 2
	exitErr   = 1
)

// run is the testable entry point. It returns an exit code rather than calling
// os.Exit so main_test.go can drive subcommands without spawning a process.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) >= 1 {
		switch args[0] {
		case "version":
			_, _ = fmt.Fprintln(stdout, "snooze-server", version.String())
			return exitOK
		case "migrate-config":
			return runMigrateConfig(args[1:], stdout, stderr)
		case "root-token":
			return runRootToken(args[1:], stdout, stderr)
		case "help", "-h", "--help":
			printUsage(stdout)
			return exitOK
		}
	}

	return runDaemon(args, stdout, stderr)
}

// printUsage describes the supported subcommands.
func printUsage(w io.Writer) {
	_, _ = fmt.Fprintln(w, `snooze-server — Snooze monitoring server

Usage:
  snooze-server [--config /etc/snooze/server-go]   Start the daemon
  snooze-server version                            Print version and exit
  snooze-server migrate-config --from <dir>        Convert legacy Python config (placeholder)
  snooze-server root-token [--socket <path>]       Read the one-shot root token`)
}

// runMigrateConfig is a deferred placeholder: the legacy Python config layout
// is already loaded transparently by config.Load when its file names match
// sectionFiles, so the standalone migration step is on the roadmap rather
// than the critical path.
func runMigrateConfig(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("migrate-config", flag.ContinueOnError)
	fs.SetOutput(stderr)
	from := fs.String("from", "", "directory containing legacy Python YAML files")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if *from == "" {
		_, _ = fmt.Fprintln(stderr, "migrate-config: --from is required")
		return exitUsage
	}
	_, _ = fmt.Fprintln(stdout, "migrate-config: not yet implemented; the loader already accepts the legacy file names verbatim")
	return exitOK
}

// runRootToken talks to the admin Unix socket and prints the minted root JWT.
func runRootToken(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("root-token", flag.ContinueOnError)
	fs.SetOutput(stderr)
	socket := fs.String("socket", defaultAdminSocketPath(), "path to the admin socket")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	hc := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", *socket)
			},
		},
		Timeout: 5 * time.Second,
	}
	resp, err := hc.Get("http://admin/api/root_token")
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "root-token: dial %s: %v\n", *socket, err)
		return exitErr
	}
	defer resp.Body.Close() //nolint:errcheck
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		_, _ = fmt.Fprintf(stderr, "root-token: server returned %d: %s\n", resp.StatusCode, strings.TrimSpace(string(body)))
		return exitErr
	}
	_, _ = stdout.Write(body)
	if len(body) == 0 || body[len(body)-1] != '\n' {
		_, _ = fmt.Fprintln(stdout)
	}
	return exitOK
}

// defaultAdminSocketPath mirrors the canonical packaging path.
func defaultAdminSocketPath() string { return "/var/run/snooze/admin.sock" }

// daemonFlags captures the CLI knobs that influence the daemon (as opposed to
// subcommands).
type daemonFlags struct {
	configDir    string
	listenAddr   string
	adminSock    string
	logFormat    string
	logLevel     string
	skipAdmin    bool
	skipHTTP     bool
	otelEndpoint string
	webDir       string
}

// parseDaemonFlags parses the no-subcommand path. Unknown flags surface as an
// error to the caller; the help text is always emitted on -h/--help.
func parseDaemonFlags(args []string, stderr io.Writer) (*daemonFlags, error) {
	fs := flag.NewFlagSet("snooze-server", flag.ContinueOnError)
	fs.SetOutput(stderr)
	f := &daemonFlags{}
	fs.StringVar(&f.configDir, "config", "/etc/snooze/server-go", "directory containing YAML config files")
	fs.StringVar(&f.listenAddr, "listen", "", "override core.listen_addr:port (host:port)")
	fs.StringVar(&f.adminSock, "admin-socket", "", "override admin socket path (default /var/run/snooze/admin.sock)")
	fs.StringVar(&f.logFormat, "log-format", "json", "log format: json or text")
	fs.StringVar(&f.logLevel, "log-level", "info", "log level: debug, info, warn, error")
	fs.BoolVar(&f.skipAdmin, "no-admin-socket", false, "disable the admin Unix socket")
	fs.BoolVar(&f.skipHTTP, "no-http", false, "disable the public HTTP listener (debug only)")
	fs.StringVar(&f.otelEndpoint, "otel-endpoint", "", "OTLP trace exporter endpoint (defaults: disabled)")
	fs.StringVar(&f.webDir, "web-dir", "/var/lib/snooze/web", "directory containing the built web UI (web/dist contents); empty disables /web")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	return f, nil
}

// runDaemon drives the full bring-up sequence. The blocking wait for shutdown
// signals happens inside; the function returns once every subsystem has
// reported back.
func runDaemon(args []string, _ io.Writer, stderr io.Writer) int {
	f, err := parseDaemonFlags(args, stderr)
	if err != nil {
		// flag has already printed the message.
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}

	// Root context wired to SIGINT/SIGTERM. We use signal.NotifyContext so
	// every subsystem can observe the cancellation through Context.Done().
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := runDaemonCtx(ctx, f, stderr); err != nil {
		_, _ = fmt.Fprintf(stderr, "snooze-server: %v\n", err)
		return exitErr
	}
	return exitOK
}

// runDaemonCtx is the parts of runDaemon that are exercised by tests directly.
// It blocks until ctx is cancelled or a subsystem returns a fatal error.
func runDaemonCtx(ctx context.Context, f *daemonFlags, stderr io.Writer) error {
	cfg, err := config.Load(f.configDir)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	applyFlagOverrides(cfg, f)

	loggers, err := telemetry.Init(telemetry.LoggerConfig{
		Level:  f.logLevel,
		Format: telemetry.LogFormat(f.logFormat),
		Output: stderr,
	})
	if err != nil {
		return fmt.Errorf("init loggers: %w", err)
	}

	// InitTracer always wires the global TracerProvider, even when no OTLP
	// endpoint is configured. We only invoke it when the operator wants
	// remote export — the default zero-value provider returned by otel is
	// fine for in-process tracing, and skipping the SDK boot keeps the
	// daemon resource graph small (and avoids the schema-merge dance in
	// `resource.Default()`).
	if f.otelEndpoint != "" {
		shutdownTrace, err := telemetry.InitTracer(ctx, telemetry.TracingConfig{
			Endpoint:    f.otelEndpoint,
			ServiceName: "snooze-server",
			Insecure:    true,
		})
		if err != nil {
			return fmt.Errorf("init tracer: %w", err)
		}
		defer func() {
			shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = shutdownTrace(shutCtx)
		}()
	}

	promReg := prometheus.NewRegistry()
	metrics := telemetry.NewRegistry(promReg)

	drv, err := openDB(ctx, cfg.Core.Database, loggers.Snooze)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() { _ = drv.Close() }()

	c, err := core.New(ctx, cfg, drv, loggers, metrics)
	if err != nil {
		return fmt.Errorf("core: %w", err)
	}

	providers := buildAuthProviders(cfg, drv, c.Settings)

	adapter := &coreAdapter{Core: c}
	rt := &api.Router{
		Auth:            c.Tokens,
		Refresh:         c.Refresh,
		Plugins:         c.Plugins(),
		Host:            c,
		DB:              drv,
		Logger:          loggers.API,
		AuditLog:        loggers.Audit,
		Metrics:         metrics,
		MetricsGatherer: promReg,
		Tracer:          c.Trc,
		Config:          cfg,
		Providers:       providers,
		Processor:       adapter,
		CORSConfig:      corsFromConfig(cfg.Core.CORS),
		WebFS:           openWebFS(f.webDir, loggers.API),
	}
	handler := rt.Build()

	httpCfg := apiServerConfig(cfg.Core)
	httpSrv := api.NewServer(httpCfg, handler, loggers.API)

	g, gctx := errgroup.WithContext(ctx)

	// Public HTTP listener.
	if !f.skipHTTP {
		g.Go(func() error {
			if err := httpSrv.ListenAndServe(gctx); err != nil {
				return fmt.Errorf("api: %w", err)
			}
			return nil
		})
	}

	// Admin socket: serves /api/root_token. The packaging socket path is
	// outside any sandbox; in tests we override via -admin-socket.
	if !f.skipAdmin {
		adminPath := f.adminSock
		if adminPath == "" {
			adminPath = defaultAdminSocketPath()
		}
		admin := &api.AdminServer{
			Path:        adminPath,
			Tokens:      c.Tokens,
			UID:         os.Getuid(),
			AllowedUIDs: []int{os.Getuid()},
			Logger:      loggers.API,
		}
		g.Go(func() error {
			if err := admin.ListenAndServe(gctx); err != nil {
				return fmt.Errorf("admin: %w", err)
			}
			return nil
		})
	}

	// Core subsystems (asyncwriter, housekeeper, syncer, node heartbeat).
	g.Go(func() error {
		if err := c.Run(gctx); err != nil {
			return fmt.Errorf("core: %w", err)
		}
		return nil
	})

	if err := g.Wait(); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

// applyFlagOverrides folds command-line flags into the loaded config.
//
// We treat the CLI as a thin convenience layer for the most common operator
// overrides (listen address + admin socket path) rather than a parallel
// configuration system; everything else flows through the YAML/env path.
func applyFlagOverrides(cfg *config.Config, f *daemonFlags) {
	if f.listenAddr != "" {
		if host, port, ok := splitHostPort(f.listenAddr); ok {
			cfg.Core.ListenAddr = host
			cfg.Core.Port = port
		}
	}
}

// splitHostPort parses host:port into its components. Invalid input returns
// !ok; the caller keeps the existing config value.
func splitHostPort(s string) (string, int, bool) {
	host, portStr, err := net.SplitHostPort(s)
	if err != nil {
		return "", 0, false
	}
	var port int
	if _, err := fmt.Sscanf(portStr, "%d", &port); err != nil {
		return "", 0, false
	}
	if port <= 0 || port > 65535 {
		return "", 0, false
	}
	return host, port, true
}

// openDB dispatches to the configured database driver. The "file" backend is
// the legacy Python alias for SQLite — the Go port stores documents in
// SQLite/JSON1 regardless of whether the operator wrote "file" or "sqlite".
func openDB(ctx context.Context, dbcfg schema.Database, logger *slog.Logger) (db.Driver, error) {
	switch strings.ToLower(strings.TrimSpace(dbcfg.Type)) {
	case "", "file", "sqlite":
		path := dbcfg.Path
		if path == "" {
			path = "./db.sqlite"
		}
		return sqlite.New(ctx, sqlite.Config{Path: path})
	case "postgres", "pg":
		return postgres.New(ctx, postgres.Config{
			DSN:             dbcfg.DSN,
			PoolMin:         dbcfg.PoolMinSize,
			PoolMax:         dbcfg.PoolMaxSize,
			ApplicationName: "snooze-server",
		})
	case "mongo", "mongodb":
		uri := dbcfg.DSN
		if uri == "" {
			if s, ok := dbcfg.Host.(string); ok {
				uri = s
			}
		}
		return mongo.New(ctx, mongo.Config{
			URI:      uri,
			Database: dbcfg.Database,
			Logger:   logger,
		})
	default:
		return nil, fmt.Errorf("unknown database type %q", dbcfg.Type)
	}
}

// openWebFS resolves the directory containing the built web UI into an
// http.FileSystem suitable for api.Router.WebFS. Empty path or a missing
// directory yields nil — the SPA stub in internal/api/routes_static.go is
// then served at /web/.
func openWebFS(dir string, logger *slog.Logger) http.FileSystem {
	if dir == "" {
		return nil
	}
	info, err := os.Stat(dir) //nolint:gosec
	if err != nil || !info.IsDir() {
		if logger != nil {
			logger.Warn("web ui directory missing; serving stub",
				slog.String("web_dir", dir),
				slog.Any("err", err))
		}
		return nil
	}
	return http.Dir(dir)
}

// buildAuthProviders wires every enabled identity provider into a fresh
// Registry. Local is always available so the bootstrap root user can log in;
// LDAP and anonymous come online when their config sections are enabled.
//
// The LDAP provider is always registered when “settings“ includes an
// “ldap.enabled“ row, OR when the file-config baseline has it enabled —
// the actual “enabled“ check happens on every Authenticate call so the
// UI's "flip the switch" path takes effect without a restart. The
// anonymous provider stays gated on the bootstrap config because it has
// no settings-form representation today.
func buildAuthProviders(cfg *config.Config, drv db.Driver, rs *config.RuntimeSettings) *auth.Registry {
	reg := auth.NewRegistry()
	// Local is always registered so the bootstrap root user can authenticate
	// even if the operator has hidden the Local tab from the login screen
	// (general.local_enabled=false). The /login backend index filters it out
	// via the EnableChecker; Authenticate keeps working.
	local := auth.NewLocalProvider(drv)
	local.Enabled = cfg.General.LocalEnabled
	reg.Register(local)
	// LDAP is always registered so a runtime ldap.enabled=true edit becomes
	// effective without a restart. The provider's IsEnabled reads the live
	// config so the login screen flips automatically.
	reg.Register(auth.NewLDAPProvider(func(ctx context.Context) (schema.LDAP, error) {
		if rs == nil {
			return cfg.LDAP, nil
		}
		return rs.LDAP(ctx)
	}))
	if cfg.General.AnonymousEnabled {
		reg.Register(auth.NewAnonymousProvider(true))
	}
	return reg
}

// corsFromConfig builds a middleware.CORSConfig from the koanf section. Nil is
// returned when the config holds the canonical defaults so the router can use
// its built-in policy.
func corsFromConfig(c schema.CORS) *middleware.CORSConfig {
	if c.AllowOrigins == "" && c.AllowCredentials == "" {
		return nil
	}
	out := middleware.DefaultCORS()
	if c.AllowOrigins != "" {
		out.AllowOrigins = splitCSV(c.AllowOrigins)
	}
	if c.AllowCredentials != "" {
		out.AllowCredentials = strings.EqualFold(strings.TrimSpace(c.AllowCredentials), "true")
	}
	return &out
}

// splitCSV explodes a comma-separated list, trimming whitespace. The single
// star wildcard short-circuits to a single-element slice.
func splitCSV(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" || s == "*" {
		return []string{"*"}
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// apiServerConfig translates the bootstrap config into the api.Config struct.
func apiServerConfig(c schema.Core) api.Config {
	tls := api.TLSConfig{}
	if c.SSL.Enabled {
		tls = api.TLSConfig{
			Enabled:  true,
			CertFile: c.SSL.CertFile,
			KeyFile:  c.SSL.KeyFile,
		}
	}
	return api.Config{
		Addr:         fmt.Sprintf("%s:%d", c.ListenAddr, c.Port),
		TLS:          tls,
		GracePeriod:  30 * time.Second,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
