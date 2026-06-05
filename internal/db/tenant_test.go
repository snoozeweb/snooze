// internal/db/tenant_test.go
package db_test

import (
	"context"
	"errors"
	"testing"

	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

func TestTenantScope_globalCollection(t *testing.T) {
	ctx := snoozetypes.WithTenant(context.Background(), "acme")
	tenantID, inject, err := db.TenantScope(ctx, "tenant")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inject {
		t.Fatal("global collection must not inject")
	}
	if tenantID != "" {
		t.Fatalf("tenantID must be empty for global collection, got %q", tenantID)
	}
}

func TestTenantScope_platformScope(t *testing.T) {
	ctx := snoozetypes.WithPlatformScope(context.Background())
	tenantID, inject, err := db.TenantScope(ctx, "record")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inject {
		t.Fatal("platform scope must not inject")
	}
	if tenantID != "" {
		t.Fatalf("tenantID must be empty for platform scope, got %q", tenantID)
	}
}

func TestTenantScope_withTenant(t *testing.T) {
	ctx := snoozetypes.WithTenant(context.Background(), "acme")
	tenantID, inject, err := db.TenantScope(ctx, "record")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !inject {
		t.Fatal("scoped collection with tenant: expected inject=true")
	}
	if tenantID != "acme" {
		t.Fatalf("tenantID: got %q want %q", tenantID, "acme")
	}
}

func TestTenantScope_failClosed(t *testing.T) {
	_, _, err := db.TenantScope(context.Background(), "record")
	if err == nil {
		t.Fatal("naked context on scoped collection: expected ErrNoTenant")
	}
	if !errors.Is(err, snoozetypes.ErrNoTenant) {
		t.Fatalf("error must wrap ErrNoTenant, got: %v", err)
	}
}

func TestWithTenantCond_bare(t *testing.T) {
	cond := db.WithTenantCond(condition.Cond{}, "acme")
	if cond.Op != condition.OpEq {
		t.Fatalf("bare zero cond: expected single Equals, got op %q", cond.Op)
	}
	if cond.Field != "tenant_id" {
		t.Fatalf("field: got %q want %q", cond.Field, "tenant_id")
	}
	if cond.Value != "acme" {
		t.Fatalf("value: got %v want %q", cond.Value, "acme")
	}
}

func TestWithTenantCond_wraps(t *testing.T) {
	user := condition.Equals("name", "bob")
	cond := db.WithTenantCond(user, "acme")
	if cond.Op != condition.OpAnd {
		t.Fatalf("non-zero cond: expected AND, got op %q", cond.Op)
	}
	if len(cond.Children) != 2 {
		t.Fatalf("AND children: got %d want 2", len(cond.Children))
	}
}
