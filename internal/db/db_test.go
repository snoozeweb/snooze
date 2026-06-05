package db_test

import (
	"testing"

	"github.com/snoozeweb/snooze/internal/db"
)

func TestIsGlobalCollection_seeded(t *testing.T) {
	for _, name := range []string{"tenant", "secrets", "nodes", "heartbeat"} {
		if !db.IsGlobalCollection(name) {
			t.Errorf("IsGlobalCollection(%q): expected true", name)
		}
	}
}

func TestIsGlobalCollection_tenantScoped(t *testing.T) {
	for _, name := range []string{"record", "rule", "user", "role", "snooze", "notification"} {
		if db.IsGlobalCollection(name) {
			t.Errorf("IsGlobalCollection(%q): expected false for tenant-scoped collection", name)
		}
	}
}

func TestRegisterGlobalCollection(t *testing.T) {
	db.RegisterGlobalCollection("test_global_xyz")
	if !db.IsGlobalCollection("test_global_xyz") {
		t.Fatal("RegisterGlobalCollection: collection not found after registration")
	}
}

func TestRegisterGlobalCollection_idempotent(t *testing.T) {
	db.RegisterGlobalCollection("idem_test")
	db.RegisterGlobalCollection("idem_test")
	if !db.IsGlobalCollection("idem_test") {
		t.Fatal("idempotent register failed")
	}
}
