// internal/api/middleware/tenant_resolver.go
package middleware

import (
	"sync/atomic"
	"unsafe"

	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// TenantResolver is a lock-free, read-mostly map from ingest token → tenant
// slug. It is replaced atomically on reload so readers never block writers.
// The zero value is valid; Lookup on a zero-value resolver always returns
// (snoozetypes.DefaultTenant, false).
type TenantResolver struct {
	// p is an *map[string]string stored as an unsafe.Pointer so it can be
	// updated with atomic.StorePointer without holding a mutex on the hot
	// read path.
	p unsafe.Pointer
}

// NewTenantResolver returns an empty resolver ready for use.
func NewTenantResolver() *TenantResolver {
	r := &TenantResolver{}
	m := make(map[string]string)
	atomic.StorePointer(&r.p, unsafe.Pointer(&m))
	return r
}

// Replace swaps the token→tenant table atomically. The replacement takes
// effect for all subsequent Lookup calls. Pass a freshly-built map each time;
// Replace takes ownership and never mutates the slice itself.
func (r *TenantResolver) Replace(m map[string]string) {
	// Make a defensive copy so the caller cannot mutate via the original map.
	cp := make(map[string]string, len(m))
	for k, v := range m {
		cp[k] = v
	}
	atomic.StorePointer(&r.p, unsafe.Pointer(&cp))
}

// Lookup returns the tenant slug for the given ingest token.
//   - ok=true, tenant=slug  → known token; use the slug.
//   - ok=false, tenant=DefaultTenant → unknown/empty token; fall back to default.
func (r *TenantResolver) Lookup(token string) (tenant string, ok bool) {
	if token == "" {
		return snoozetypes.DefaultTenant, false
	}
	ptr := atomic.LoadPointer(&r.p)
	if ptr == nil {
		return snoozetypes.DefaultTenant, false
	}
	m := *(*map[string]string)(ptr)
	slug, found := m[token]
	if !found {
		return snoozetypes.DefaultTenant, false
	}
	return slug, true
}
