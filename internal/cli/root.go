// Package cli implements the `snooze` command-line tool. It is split into
// small per-subcommand files for ease of testing; `cmd/snooze/main.go` is a
// thin wrapper around Execute below.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/snoozeweb/snooze/internal/version"
	"github.com/snoozeweb/snooze/pkg/snoozeclient"
)

// globalFlags captures the flags wired on the root command. They are shared
// across every subcommand through the persistent flag set on the root.
type globalFlags struct {
	Server   string
	User     string
	Password string
	Token    string
	Insecure bool
	Cache    string
	Timeout  time.Duration
	JSON     bool
	Method   string
}

// runtime bundles wired-up dependencies passed down through cobra context.
// Tests inject a fake httpClient / clientFactory; production wiring is the
// default returned by defaultRuntime().
type runtime struct {
	flags *globalFlags
	// fileConfig is the parsed /etc/snooze/client.yaml (or whichever path
	// LoadClientConfig found). Its values seed flag defaults below env-var
	// lookups so an operator can keep server + creds in one place. Tests
	// inject their own to exercise the precedence chain without touching
	// the filesystem.
	fileConfig ClientConfig
	// out / errOut let tests capture cobra output. Always honour cmd.OutOrStdout()
	// inside RunE — these are convenience aliases.
	out, errOut io.Writer
	in          io.Reader
	// clientFactory builds a snoozeclient.Client from the resolved flags. Tests
	// override this with one that points at httptest.NewServer.
	clientFactory func(opts snoozeclient.Options) (*snoozeclient.Client, error)
	// httpClient is the *http.Client used for raw calls (admin socket / verbose
	// health probes that the SDK does not cover). Tests inject a fake.
	httpClient *http.Client
	// passwordReader, when set, replaces interactive password prompting in
	// `snooze login`. Always non-nil in tests.
	passwordReader func() (string, error)
}

// runtimeKey is the unexported key under which the runtime is stashed on the
// cobra command's context.
type runtimeKey struct{}

// withRuntime attaches rt to ctx so subcommand RunE handlers can retrieve it.
func withRuntime(ctx context.Context, rt *runtime) context.Context {
	return context.WithValue(ctx, runtimeKey{}, rt)
}

// runtimeFrom returns the runtime attached by NewRootCmd. It always returns
// a non-nil value; if the context lacks one we fall back to a defaulted
// runtime so partial tests still work.
func runtimeFrom(ctx context.Context) *runtime {
	if rt, ok := ctx.Value(runtimeKey{}).(*runtime); ok && rt != nil {
		return rt
	}
	return defaultRuntime()
}

// defaultRuntime returns the production runtime: real snoozeclient.New, real
// http.DefaultClient, terminal-backed password prompt.
func defaultRuntime() *runtime {
	return &runtime{
		flags:          &globalFlags{},
		fileConfig:     LoadClientConfig(),
		out:            os.Stdout,
		errOut:         os.Stderr,
		in:             os.Stdin,
		clientFactory:  snoozeclient.New,
		httpClient:     http.DefaultClient,
		passwordReader: nil, // wired by login.go on demand
	}
}

// NewRootCmd builds the root `snooze` cobra command. rt is optional; when nil
// a defaultRuntime is constructed. Callers that need to inject dependencies
// (tests) pass their own runtime here.
func NewRootCmd(rt *runtime) *cobra.Command {
	if rt == nil {
		rt = defaultRuntime()
	}
	if rt.flags == nil {
		rt.flags = &globalFlags{}
	}

	cmd := &cobra.Command{
		Use:           "snooze",
		Short:         "Snooze CLI client",
		Long:          "The snooze CLI talks to a Snooze server's REST API (/api/v1/*).",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       version.String(),
	}

	// Capture the command's IO so subcommands can use cmd.OutOrStdout() and
	// honour the runtime overrides when present.
	if rt.out != nil {
		cmd.SetOut(rt.out)
	}
	if rt.errOut != nil {
		cmd.SetErr(rt.errOut)
	}
	if rt.in != nil {
		cmd.SetIn(rt.in)
	}

	// --- global flags ------------------------------------------------------
	//
	// Tests pre-populate rt.flags with their own values (e.g. Server pointed
	// at an httptest.Server URL). pflag's StringVar would clobber those at
	// registration time, so we seed the *defaults* from whatever was already
	// stashed and only fall back to env vars / config-file / hard-coded
	// values when the caller left the field zero.
	//
	// Precedence per flag is:
	//
	//   1. rt.flags pre-population (tests / programmatic callers).
	//   2. matching SNOOZE_* env var.
	//   3. /etc/snooze/client.yaml (via rt.fileConfig — see LoadClientConfig).
	//   4. hard-coded fallback.
	f := rt.flags
	cfg := rt.fileConfig
	pf := cmd.PersistentFlags()
	pf.StringVar(&f.Server, "server",
		nonEmpty(f.Server, envOrDefault("SNOOZE_SERVER", nonEmpty(cfg.Server, "http://localhost:5200"))),
		"Snooze server base URL")
	pf.StringVar(&f.User, "user",
		nonEmpty(f.User, envOrDefault("SNOOZE_USER", cfg.Credentials.Username)),
		"Username for login")
	pf.StringVar(&f.Password, "password",
		nonEmpty(f.Password, envOrDefault("SNOOZE_PASSWORD", cfg.Credentials.Password)),
		"Password for login (prompts if empty)")
	pf.StringVar(&f.Token, "token",
		nonEmpty(f.Token, os.Getenv("SNOOZE_TOKEN")),
		"Bearer token (skips login flow)")
	insecureDefault := f.Insecure || cfg.Insecure
	pf.BoolVar(&f.Insecure, "insecure", insecureDefault, "Skip TLS certificate verification")
	pf.StringVar(&f.Cache, "cache",
		nonEmpty(f.Cache, os.Getenv("SNOOZE_TOKEN_CACHE")),
		"Path to the token cache file (defaults to OS cache)")
	timeoutDefault := f.Timeout
	if timeoutDefault <= 0 {
		timeoutDefault = cfg.Timeout
	}
	if timeoutDefault <= 0 {
		timeoutDefault = 30 * time.Second
	}
	pf.DurationVar(&f.Timeout, "timeout", timeoutDefault, "HTTP request timeout")
	pf.BoolVar(&f.JSON, "json", f.JSON, "Emit JSON instead of the human-readable format")
	pf.StringVar(&f.Method, "method",
		nonEmpty(f.Method, envOrDefault("SNOOZE_METHOD", nonEmpty(cfg.Method, "local"))),
		"Auth method (local, ldap, anonymous)")

	// Stash runtime on the command's context so RunE handlers can grab it.
	cmd.SetContext(withRuntime(context.Background(), rt))

	// --- subcommands -------------------------------------------------------
	cmd.AddCommand(
		newLoginCmd(),
		newRecordCmd(),
		newSnoozeCmd(),
		newHealthCmd(),
		newRootTokenCmd(),
		newQueryCmd(),
		newVersionCmd(),
	)
	return cmd
}

// Execute is the entry point invoked from cmd/snooze/main.go. It builds the
// command tree, parses os.Args, and translates errors into a non-zero exit
// code with a stderr message.
func Execute() int {
	rt := defaultRuntime()
	root := NewRootCmd(rt)
	if err := root.ExecuteContext(root.Context()); err != nil {
		_, _ = fmt.Fprintln(rt.errOut, "snooze:", err)
		return 1
	}
	return 0
}

// envOrDefault returns os.Getenv(key) if non-empty, otherwise def.
func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// nonEmpty returns the first non-empty value, allowing test-injected fields
// on rt.flags to win over the production defaults.
func nonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// buildClient turns the global flags into a configured *snoozeclient.Client.
// The returned client has the token / cache wired but Login is NOT called —
// each subcommand decides whether to lazily Login or pre-authenticate.
func (rt *runtime) buildClient() (*snoozeclient.Client, error) {
	if rt.flags == nil {
		return nil, errors.New("cli: no global flags configured")
	}
	f := rt.flags
	if f.Server == "" {
		return nil, errors.New("cli: --server is required")
	}
	opts := snoozeclient.Options{
		BaseURL:        f.Server,
		Username:       f.User,
		Password:       f.Password,
		Token:          f.Token,
		Method:         f.Method,
		Timeout:        f.Timeout,
		Insecure:       f.Insecure,
		TokenCacheFile: f.Cache,
	}
	factory := rt.clientFactory
	if factory == nil {
		factory = snoozeclient.New
	}
	return factory(opts)
}

// newVersionCmd is a tiny subcommand so `snooze version` works alongside
// `--version`. It mirrors the format used by the sibling daemons.
func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the snooze version",
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "snooze", version.String())
			return nil
		},
	}
}
