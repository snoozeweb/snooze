// internal/api/ingest_reload_test.go
package api

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/api/middleware"
	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/db"
)

// stubTenantDriver returns the docs set on construction.
type stubTenantDriver struct {
	db.Driver
	docs []db.Document
}

func (s *stubTenantDriver) Search(_ context.Context, _ string, _ condition.Cond, _ db.Page) ([]db.Document, int, error) {
	return s.docs, len(s.docs), nil
}

func TestBuildIngestTokenMap_PopulatesFromDocs(t *testing.T) {
	docs := []db.Document{
		{"id": "acme", "ingest_token": "tok-acme", "status": "active"},
		{"id": "beta", "ingest_token": "tok-beta", "status": "active"},
		{"id": "suspended-co", "ingest_token": "tok-sus", "status": "suspended"},
	}
	m := buildIngestTokenMap(docs)
	require.Equal(t, "acme", m["tok-acme"])
	require.Equal(t, "beta", m["tok-beta"])
	// Suspended tenants still appear in the token map — the IngestTenant
	// middleware handles the status check separately.
	require.Equal(t, "suspended-co", m["tok-sus"])
}

func TestBuildIngestTokenMap_EmptyTokenSkipped(t *testing.T) {
	docs := []db.Document{
		{"id": "acme", "ingest_token": "", "status": "active"},
		{"id": "beta", "status": "active"}, // no ingest_token key at all
	}
	m := buildIngestTokenMap(docs)
	require.Empty(t, m)
}

func TestReloadIngestTokens_UpdatesResolver(t *testing.T) {
	driver := &stubTenantDriver{docs: []db.Document{
		{"id": "acme", "ingest_token": "tok-acme", "status": "active"},
	}}
	resolver := middleware.NewTenantResolver()

	err := reloadIngestTokens(context.Background(), driver, resolver)
	require.NoError(t, err)

	slug, ok := resolver.Lookup("tok-acme")
	require.True(t, ok)
	require.Equal(t, "acme", slug)
}
