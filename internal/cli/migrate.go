package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"
)

// migrateRunnerKey is the unexported context key holding an optional migration
// runner override — used only by tests.
type migrateRunnerKey struct{}

// withMigrateRunner stores a test-injectable runner on ctx.
func withMigrateRunner(ctx context.Context, fn func(context.Context) error) context.Context {
	return context.WithValue(ctx, migrateRunnerKey{}, fn)
}

// migrateRunnerFrom retrieves the runner override (tests) or returns nil
// (production).
func migrateRunnerFrom(ctx context.Context) func(context.Context) error {
	fn, _ := ctx.Value(migrateRunnerKey{}).(func(context.Context) error)
	return fn
}

// newMigrateCmd returns the `snooze migrate` parent command.
func newMigrateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Run one-shot database migration scripts",
	}
	cmd.AddCommand(newMigrateMultitenancyCmd())
	return cmd
}

// newMigrateMultitenancyCmd returns the `snooze migrate multitenancy` command.
//
// The migration needs a direct database connection, which this operator CLI
// (an HTTP client to a remote server) does not have. The real entry point is
// `snooze-server migrate multitenancy`, which loads the server config, opens
// the configured driver, and calls migrate.RunMultitenancyMigration. This
// command therefore redirects the operator there. Tests inject a runner
// override via withMigrateRunner to exercise the success/error reporting.
func newMigrateMultitenancyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "multitenancy",
		Short: "Backfill tenant_id='default' on all existing documents (one-shot, idempotent)",
		Long: `Backfills the multitenancy schema on an existing database:

  1. Creates the 'default' tenant registry document.
  2. Creates the 'platform_admin' role with rw_tenant/ro_tenant permissions.
  3. Stamps tenant_id="default" in place on every document in tenant-scoped
     collections (users and roles included, completing their compound keys).
  4. Grants the root user the platform_admin role.

The migration is idempotent and dedup-safe (it updates rows in place rather
than rewriting them). A completion sentinel prevents re-execution.

This operator CLI talks to the server over HTTP and has no direct database
connection, so it cannot run the migration itself. Run it on the server host,
where the database DSN/credentials live:

    snooze-server migrate multitenancy --config /etc/snooze/server-go`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()

			// Tests inject a runner to exercise the success/error reporting
			// without a live database. Production has no runner: the operator
			// CLI has no DB connection, so we point them at the server command.
			runner := migrateRunnerFrom(ctx)
			if runner == nil {
				return errors.New(
					"this operator CLI has no direct database connection; " +
						"run the migration on the server host instead: " +
						"snooze-server migrate multitenancy --config /etc/snooze/server-go")
			}

			if err := runner(ctx); err != nil {
				return err
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "multitenancy migration complete")
			return nil
		},
	}
}
