// Package protected implements Snooze's protected-field registry.
//
// A protected field is a document key that only a dedicated, schema-validating
// endpoint may write. Everything else in the system — alert ingestion, rule
// modifications, the generic CRUD surface, the bulk endpoints — treats it as
// read-only: reads always see it, writes never touch it.
//
// The motivating case is `agentic`, the AI-authored root-cause / remediation
// analysis attached to an alert (see internal/api/routes_agentic.go). That
// subtree has a strict schema and an audit trail; letting an inbound alert
// payload or a rule's SET modification fabricate it would defeat both.
//
// # Semantics
//
//   - Protection is by TOP-LEVEL field name and is RECURSIVE: protecting
//     `agentic` protects `agentic.root_cause.summary` and every other
//     descendant, because the only way to reach a child is through the
//     protected parent.
//   - Protection is collection-agnostic. The registry names fields, not
//     (collection, field) pairs — a protected name is protected everywhere,
//     which keeps the guards free of collection plumbing.
//   - The registry is seeded at init and may be extended by Register during
//     boot. It is not operator-configurable: an operator-editable protection
//     list would be one config mistake away from silently unprotecting the
//     very data the concept exists to protect.
//
// The only sanctioned writer is a handler that goes through the driver
// directly after validating the payload, gated on WritePermission.
package protected

import (
	"bytes"
	"encoding/json"
	"sort"
	"strings"
	"sync"
)

// WritePermission is the literal permission a caller must hold to write a
// protected field. It is deliberately NOT implied by the `rw_all` admin
// wildcard (see auth.HasLiteralPermission): inheriting it silently would give
// every existing admin role write access to agentic data, which is exactly
// what the concept is meant to prevent. Grant it explicitly to the identities
// that run analysis agents.
const WritePermission = "rw_protected"

// AgenticField is the one field protected out of the box: the AI-authored
// analysis subtree on a record.
const AgenticField = "agentic"

var (
	mu       sync.RWMutex
	registry = map[string]struct{}{AgenticField: {}}
)

// Register adds a top-level field name to the protected set. It is intended
// for boot-time wiring; calling it after the server is serving traffic is
// safe (the registry is mutex-guarded) but means earlier writes were not
// guarded. A blank name is ignored.
func Register(field string) {
	field = strings.TrimSpace(field)
	if field == "" {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	registry[field] = struct{}{}
}

// Fields returns the registered protected field names, sorted, so callers can
// render deterministic error messages and tests stay reproducible.
func Fields() []string {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]string, 0, len(registry))
	for f := range registry {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

// IsProtected reports whether path addresses a protected field or any of its
// descendants. It accepts both a bare field name ("agentic") and a dotted
// path ("agentic.root_cause.summary"); only the first segment is consulted,
// which is what makes protection recursive.
//
// Surrounding whitespace is trimmed so a rule whose modification target was
// typed as " agentic " cannot slip past the guard.
func IsProtected(path string) bool {
	head := strings.TrimSpace(path)
	if i := strings.IndexByte(head, '.'); i >= 0 {
		head = head[:i]
	}
	head = strings.TrimSpace(head)
	if head == "" {
		return false
	}
	mu.RLock()
	defer mu.RUnlock()
	_, ok := registry[head]
	return ok
}

// Names returns the protected keys present in doc, sorted. An empty result
// means the document is safe to write through an unprivileged path.
func Names(doc map[string]any) []string {
	if len(doc) == 0 {
		return nil
	}
	var out []string
	for k := range doc {
		if IsProtected(k) {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// Strip removes every protected key from doc in place and returns the names it
// removed, sorted. This is the ingestion-side behaviour: an inbound alert that
// carries a protected field is accepted, minus that field, rather than being
// rejected — a noisy sender must not be able to break its own alerting by
// guessing a protected name.
func Strip(doc map[string]any) []string {
	removed := Names(doc)
	for _, k := range removed {
		delete(doc, k)
	}
	return removed
}

// Carry copies the protected keys of src into dst, overwriting whatever dst
// holds under those names. It is the full-replace counterpart of Strip: a PUT
// that replaces a document wholesale must not be able to delete protected data
// by simply omitting it, so the stored values are carried forward.
func Carry(dst, src map[string]any) {
	if dst == nil || len(src) == 0 {
		return
	}
	for _, k := range Names(src) {
		dst[k] = src[k]
	}
}

// Same reports whether two values of a protected field are the same value,
// comparing them through their JSON encoding rather than with ==. That
// normalisation is the point: a value read back from a driver and the same
// value decoded from a request body routinely differ in Go type — int64 vs
// float64 for a number, map[string]any vs bson.M for an object — while being
// indistinguishable over the wire, which is the only representation the API
// contract actually promises.
//
// It is what lets a read-modify-write client echo a stored protected subtree
// back in a PUT/PATCH: an echo is a no-op, and refusing a no-op protects
// nothing while making the generic CRUD surface unusable on every document
// that carries an analysis.
//
// A value that cannot be marshalled (a channel, a func, a cyclic structure)
// is never "the same" as anything — fail closed and let the caller refuse.
func Same(a, b any) bool {
	ja, err := json.Marshal(a)
	if err != nil {
		return false
	}
	jb, err := json.Marshal(b)
	if err != nil {
		return false
	}
	// encoding/json sorts object keys, so byte equality is key-order
	// independent for maps — which is all a decoded document contains.
	return bytes.Equal(ja, jb)
}
