package cli

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMigrateMultitenancyCmd_Success(t *testing.T) {
	var stdout, stderr bytes.Buffer
	called := false
	rt := &runtime{
		flags:  &globalFlags{Server: "http://example.invalid"},
		out:    &stdout,
		errOut: &stderr,
	}
	root := NewRootCmd(rt)
	// Inject a no-op runner via the context hack used by other CLI tests.
	root.SetContext(withMigrateRunner(withRuntime(context.Background(), rt), func(_ context.Context) error {
		called = true
		return nil
	}))
	root.SetArgs([]string{"migrate", "multitenancy"})
	require.NoError(t, root.Execute())
	require.True(t, called, "migration runner must be called")
	require.Contains(t, stdout.String(), "migration complete")
}

func TestMigrateMultitenancyCmd_RunnerError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rt := &runtime{
		flags:  &globalFlags{Server: "http://example.invalid"},
		out:    &stdout,
		errOut: &stderr,
	}
	root := NewRootCmd(rt)
	root.SetContext(withMigrateRunner(withRuntime(context.Background(), rt), func(_ context.Context) error {
		return errors.New("db: connection refused")
	}))
	root.SetArgs([]string{"migrate", "multitenancy"})
	err := root.Execute()
	require.Error(t, err)
	require.Contains(t, err.Error(), "connection refused")
}

// TestMigrateMultitenancyCmd_NoRunner_RedirectsToServer asserts that the
// production path (no test runner injected) does not attempt to run the
// migration with a nil driver — the operator CLI is an HTTP client with no
// database connection — and instead returns a clear error pointing at the
// `snooze-server migrate multitenancy` command, which owns the DB.
func TestMigrateMultitenancyCmd_NoRunner_RedirectsToServer(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rt := &runtime{
		flags:  &globalFlags{Server: "http://example.invalid"},
		out:    &stdout,
		errOut: &stderr,
	}
	root := NewRootCmd(rt)
	// No migrate runner injected → production path.
	root.SetContext(withRuntime(context.Background(), rt))
	root.SetArgs([]string{"migrate", "multitenancy"})
	err := root.Execute()
	require.Error(t, err)
	require.Contains(t, err.Error(), "snooze-server migrate multitenancy")
}

func TestMigrateCmd_Help(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rt := &runtime{
		flags:  &globalFlags{Server: "http://example.invalid"},
		out:    &stdout,
		errOut: &stderr,
	}
	root := NewRootCmd(rt)
	root.SetArgs([]string{"migrate", "--help"})
	require.NoError(t, root.Execute())
	require.Contains(t, stdout.String(), "multitenancy")
}
