// internal/api/ingest_reload.go
package api

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/snoozeweb/snooze/internal/api/middleware"
	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/pluginimpl/tenant"
)

// buildIngestTokenMap builds a token→tenantID map from a slice of tenant docs.
// Docs with an empty or absent ingest_token are skipped.
func buildIngestTokenMap(docs []db.Document) map[string]string {
	out := make(map[string]string, len(docs))
	for _, doc := range docs {
		id, _ := doc["id"].(string)
		tok, _ := doc["ingest_token"].(string)
		if tok == "" || id == "" {
			continue
		}
		out[tok] = id
	}
	return out
}

// reloadIngestTokens queries the tenant collection under platform scope, builds
// the token map, and atomically replaces the resolver's table. It is called at
// boot and whenever a tenant doc changes (syncer event on the "tenant" collection).
func reloadIngestTokens(ctx context.Context, driver db.Driver, resolver *middleware.TenantResolver) error {
	ctx = auth.WithPlatformScope(ctx)
	docs, _, err := driver.Search(ctx, tenant.Collection, condition.Cond{Op: condition.OpAlwaysTrue}, db.Page{})
	if err != nil {
		return fmt.Errorf("ingest: reload tokens: %w", err)
	}
	resolver.Replace(buildIngestTokenMap(docs))
	return nil
}

// StartIngestTokenReloader reloads the resolver once at startup then listens
// for syncer events on the "tenant" collection, re-loading on any change.
// It blocks until ctx is done and is intended to run in a goroutine.
func StartIngestTokenReloader(
	ctx context.Context,
	driver db.Driver,
	resolver *middleware.TenantResolver,
	sub <-chan struct{},
	logger *slog.Logger,
) {
	if logger == nil {
		logger = slog.Default()
	}
	// Initial load.
	if err := reloadIngestTokens(ctx, driver, resolver); err != nil {
		logger.Warn("ingest: initial token reload failed", slog.Any("err", err))
	}
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-sub:
			if !ok {
				return
			}
			if err := reloadIngestTokens(ctx, driver, resolver); err != nil {
				logger.Warn("ingest: token reload failed", slog.Any("err", err))
			}
		}
	}
}
