package apikey

import (
	"context"
	"testing"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/plugins"
)

func newPlugin(t *testing.T) *Plugin {
	t.Helper()
	p, err := factory(plugins.Metadata{})
	if err != nil {
		t.Fatal(err)
	}
	return p.(*Plugin)
}

func TestApiKeyPlugin_Identity(t *testing.T) {
	p := newPlugin(t)
	if p.Name() != auth.APIKeyCollection {
		t.Fatalf("Name = %q", p.Name())
	}
	if got := p.PrimaryKey(); len(got) != 3 || got[0] != "tenant_id" || got[1] != "owner" || got[2] != "name" {
		t.Fatalf("PrimaryKey = %v", got)
	}
}

func TestApiKeyPlugin_GuardWrite(t *testing.T) {
	p := newPlugin(t)
	ctx := context.Background()
	if err := p.GuardWrite(ctx, "", map[string]any{"name": "x"}, false); err == nil {
		t.Fatal("create via generic CRUD must be rejected")
	}
	if err := p.GuardWrite(ctx, "u1", map[string]any{"name": "x"}, true); err == nil {
		t.Fatal("PUT/replace must be rejected")
	}
	if err := p.GuardWrite(ctx, "u1", map[string]any{"permissions": []any{"rw_all"}}, false); err == nil {
		t.Fatal("editing permissions must be rejected")
	}
	if err := p.GuardWrite(ctx, "u1", map[string]any{"name": "renamed", "expires_at": float64(1)}, false); err != nil {
		t.Fatalf("editing name/expiry must be allowed: %v", err)
	}
}
