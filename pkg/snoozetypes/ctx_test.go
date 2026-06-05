package snoozetypes_test

import (
	"context"
	"testing"

	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

func TestWithTenant_roundtrip(t *testing.T) {
	ctx := snoozetypes.WithTenant(context.Background(), "acme")
	got, ok := snoozetypes.TenantFrom(ctx)
	if !ok {
		t.Fatal("TenantFrom: expected ok=true")
	}
	if got != "acme" {
		t.Fatalf("TenantFrom: got %q want %q", got, "acme")
	}
}

func TestTenantFrom_missing(t *testing.T) {
	_, ok := snoozetypes.TenantFrom(context.Background())
	if ok {
		t.Fatal("TenantFrom on bare context: expected ok=false")
	}
}

func TestWithPlatformScope_roundtrip(t *testing.T) {
	ctx := snoozetypes.WithPlatformScope(context.Background())
	if !snoozetypes.IsPlatformScope(ctx) {
		t.Fatal("IsPlatformScope: expected true after WithPlatformScope")
	}
}

func TestIsPlatformScope_false(t *testing.T) {
	if snoozetypes.IsPlatformScope(context.Background()) {
		t.Fatal("IsPlatformScope on bare context: expected false")
	}
}

func TestTenantAndPlatformIndependent(t *testing.T) {
	// Tenant and platform keys must not collide.
	ctx := snoozetypes.WithTenant(context.Background(), "x")
	if snoozetypes.IsPlatformScope(ctx) {
		t.Fatal("tenant context must not imply platform scope")
	}
	ctx2 := snoozetypes.WithPlatformScope(context.Background())
	if _, ok := snoozetypes.TenantFrom(ctx2); ok {
		t.Fatal("platform-scope context must not carry a tenant")
	}
}

func TestDefaultTenant(t *testing.T) {
	if snoozetypes.DefaultTenant != "default" {
		t.Fatalf("DefaultTenant: got %q want %q", snoozetypes.DefaultTenant, "default")
	}
}

func TestErrNoTenant(t *testing.T) {
	if snoozetypes.ErrNoTenant == nil {
		t.Fatal("ErrNoTenant must not be nil")
	}
}
