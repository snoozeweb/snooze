// Package migrate provides the one-shot multitenancy migration that backfills
// tenant_id = "default" on every existing document in tenant-scoped
// collections, rewrites user/role PKs to include the default tenant, creates
// the default tenant registry doc, and grants the root user the platform_admin
// role.
//
// The migration is idempotent: running it twice produces the same state as
// running it once. A sentinel document in the "general" collection marks
// completion.
//
// All operations run under auth.WithPlatformScope so the driver's tenant
// injection is bypassed (the collections are being bootstrapped for the first
// time and have no tenant_id yet).
package migrate

// migrationMarkerCollection is where the idempotency sentinel lives.
const migrationMarkerCollection = "general"

// migrationMarkerField is the key whose presence (true) signals completion.
const migrationMarkerField = "multitenancy_v1"

// TenantScopedCollections is the complete, canonical list of collections that
// must receive tenant_id = DefaultTenant during migration. Global collections
// (tenant, secrets, nodes, heartbeat) are excluded; they never carry
// tenant_id.
//
// Keep this list in sync with §2 / §4 of the Shared Contract whenever a new
// plugin adds a collection.
var TenantScopedCollections = []string{
	"record",
	"rule",
	"aggregaterule",
	"snooze",
	"notification",
	"action",
	"user",
	"role",
	"refresh_token",
	"audit",
	"stats",
	"settings",
	"comment",
	"environment",
	"kv",
	"profile",
	"widget",
	"aggregate",
	"general",
}
