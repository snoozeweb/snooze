package cli

import (
	"bytes"
	"context"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/japannext/snooze/pkg/snoozeclient"
)

// newTestRuntime builds a runtime that points the client factory at srv and
// captures all I/O into the returned buffers. The token cache is also redirected
// to a per-test tempdir so we never touch the user's real cache.
func newTestRuntime(t *testing.T, srv *httptest.Server) (*runtime, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	cacheDir := t.TempDir()
	rt := &runtime{
		flags: &globalFlags{
			Server:  srv.URL,
			Cache:   filepath.Join(cacheDir, "token"),
			Timeout: 2 * time.Second,
			Method:  "local",
		},
		out:        &stdout,
		errOut:     &stderr,
		httpClient: srv.Client(),
		clientFactory: func(opts snoozeclient.Options) (*snoozeclient.Client, error) {
			// Reuse the httptest.Server's client so TLS / dial wiring just works.
			opts.HTTPClient = srv.Client()
			opts.InitialBackoff = time.Millisecond
			opts.MaxRetries = 2
			return snoozeclient.New(opts)
		},
	}
	return rt, &stdout, &stderr
}

// executeCmd runs root with args, plumbing the runtime onto its context.
// Returns stdout / stderr / err.
func executeCmd(t *testing.T, rt *runtime, args ...string) (string, string, error) {
	t.Helper()
	root := NewRootCmd(rt)
	root.SetArgs(args)
	root.SetContext(withRuntime(context.Background(), rt))
	err := root.Execute()
	return rt.out.(*bytes.Buffer).String(), rt.errOut.(*bytes.Buffer).String(), err
}

func TestRootHelp(t *testing.T) {
	// No server needed for --help.
	var stdout, stderr bytes.Buffer
	rt := &runtime{
		flags:  &globalFlags{Server: "http://example.invalid"},
		out:    &stdout,
		errOut: &stderr,
	}
	root := NewRootCmd(rt)
	root.SetArgs([]string{"--help"})
	require.NoError(t, root.Execute())
	require.Contains(t, stdout.String(), "snooze")
	require.Contains(t, stdout.String(), "login")
	require.Contains(t, stdout.String(), "record")
}

func TestVersionSubcommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rt := &runtime{
		flags:  &globalFlags{Server: "http://example.invalid"},
		out:    &stdout,
		errOut: &stderr,
	}
	root := NewRootCmd(rt)
	root.SetArgs([]string{"version"})
	require.NoError(t, root.Execute())
	require.Contains(t, stdout.String(), "snooze")
}

func TestBuildClientRequiresServer(t *testing.T) {
	rt := &runtime{flags: &globalFlags{}}
	_, err := rt.buildClient()
	require.Error(t, err)
	require.Contains(t, err.Error(), "--server is required")
}

func TestBuildClientUsesFactory(t *testing.T) {
	called := false
	rt := &runtime{
		flags: &globalFlags{Server: "http://example.invalid"},
		clientFactory: func(opts snoozeclient.Options) (*snoozeclient.Client, error) {
			called = true
			require.Equal(t, "http://example.invalid", opts.BaseURL)
			return snoozeclient.New(opts)
		},
	}
	_, err := rt.buildClient()
	require.NoError(t, err)
	require.True(t, called, "expected the override factory to be used")
}
