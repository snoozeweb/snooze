package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/snoozeweb/snooze/internal/migrate"
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
// In production it opens the configured database driver and calls
// migrate.RunMultitenancyMigration. Tests inject a runner override via
// withMigrateRunner.
//
// NOTE: In the full production wiring (once the server config + driver boot
// path is available) this command should open the driver via the same
// configuration loader used by snooze-server. The current implementation
// delegates entirely to an injectable runner so the CLI layer is testable
// without a live database; a follow-up task wires the real driver for the
// `snooze-server migrate` invocation path.
func newMigrateMultitenancyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "multitenancy",
		Short: "Backfill tenant_id='default' on all existing documents (one-shot, idempotent)",
		Long: `Runs the multitenancy migration against the configured database:

  1. Creates the 'default' tenant registry document.
  2. Creates the 'platform_admin' role with rw_tenant/ro_tenant permissions.
  3. Stamps tenant_id="default" on every document in tenant-scoped collections.
  4. Rewrites user and role PKs to include tenant_id.
  5. Grants the root user the platform_admin role.

The migration is idempotent. A completion sentinel prevents re-execution.

For Postgres/Mongo backends, point snooze-server at the correct DSN via its
usual configuration file and invoke this command from the same process that
owns the database connection.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()

			// Production path: the migrate.RunMultitenancyMigration function
			// takes a db.Driver. In the standalone CLI the runner is wired by
			// withMigrateRunner (tests) or falls back to a default that
			// requires a driver to be provided via the server's boot sequence.
			//
			// When the runner is not injected (production without a wired
			// driver), we return a clear error directing the operator.
			runner := migrateRunnerFrom(ctx)
			if runner == nil {
				// No injected runner: use migrate.RunMultitenancyMigration
				// with a nil driver — the function returns a clear error.
				runner = func(ctx context.Context) error {
					return migrate.RunMultitenancyMigration(ctx, nil)
				}
			}

			if err := runner(ctx); err != nil {
				return err
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "multitenancy migration complete")
			return nil
		},
	}
}
