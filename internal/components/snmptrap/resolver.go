package snmptrap

import (
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"

	"github.com/sleepinggenius2/gosmi"
	gosmitypes "github.com/sleepinggenius2/gosmi/types"
)

// OIDResolver maps a dotted OID (".1.3.6.1.2.1.1.1.0") to a human-readable
// label suitable for use as a record-field key.
//
// Implementations return:
//
//   - A MIB-qualified name when a matching MIB is loaded — e.g.
//     "SNMPv2-MIB::sysDescr.0" for the example above.
//   - The original dotted OID (leading dot stripped) when no MIB matches.
//
// The returned label is NOT yet field-safe — call SanitizeKey on the
// result before using it as a JSON / MongoDB sub-document key. Keeping
// resolution and sanitization separate lets tests verify each half in
// isolation.
type OIDResolver interface {
	Resolve(dottedOID string) string
}

// NoopResolver returns the input OID with the leading dot stripped (matching
// the unresolved fallback path of every real resolver). Used in tests and when
// the operator hasn't configured any MIBs.
type NoopResolver struct{}

// Resolve trims the leading dot so the output is field-friendly. The trim is
// the only transformation NoopResolver performs.
func (NoopResolver) Resolve(oid string) string {
	return strings.TrimPrefix(oid, ".")
}

// SanitizeKey replaces every "." with "_" so the returned token is safe to use
// as a MongoDB sub-document field name (BSON pre-5.0 forbids dotted keys; 5.0+
// allows them but accessor syntax like `raw.x.y.z` becomes ambiguous, breaking
// the Snooze search DSL). Mirrors the line
//
//	record[key.replace(".", "_")] = val
//
// from the Python original (components/snmptrap/src/snooze_snmptrap/main.py).
// The "::" delimiter that gosmi.RenderQualified emits is preserved untouched
// because it doesn't trip the BSON / DSL path issue.
func SanitizeKey(name string) string {
	name = strings.TrimPrefix(name, ".")
	return strings.ReplaceAll(name, ".", "_")
}

// gosmiResolver consults gosmi's loaded MIB tree to translate a dotted OID
// into a MIB-qualified name. The mapping is cached per OID since most trap
// senders re-use the same varbind set on every packet.
type gosmiResolver struct {
	mu     sync.RWMutex
	cache  map[string]string
	logger *slog.Logger
}

// NewGosmiResolver configures gosmi's global state to load the listed
// directories and modules, then returns a resolver backed by the loaded
// catalogue.
//
// gosmi's API is process-global (gosmi.Init / gosmi.SetPath / gosmi.LoadModule
// all mutate package-level state), so callers should construct at most one
// resolver per process; constructing a second instance overwrites the first
// one's MIB view. The snmptrap daemon honours this — see daemon.go.
//
// When no modules are listed we still load the directories so a follow-up
// AppendPath / LoadModule could pick them up; the resolver remains usable but
// every OID resolves to the bare-numeric form (same as NoopResolver). An
// empty dirs slice is allowed too — the resolver falls back to gosmi's
// built-in search path.
func NewGosmiResolver(dirs []string, modules []string, logger *slog.Logger) (OIDResolver, error) {
	if logger == nil {
		logger = slog.Default()
	}
	gosmi.Init()
	for _, d := range dirs {
		if d == "" {
			continue
		}
		gosmi.AppendPath(d)
	}
	var loaded, failed int
	for _, mod := range modules {
		if mod == "" {
			continue
		}
		if _, err := gosmi.LoadModule(mod); err != nil {
			failed++
			logger.Warn("snmptrap: failed to load MIB module",
				slog.String("module", mod),
				slog.String("err", err.Error()))
			continue
		}
		loaded++
	}
	if loaded == 0 && len(modules) > 0 {
		return nil, fmt.Errorf("snmptrap: no MIB modules loaded (failed=%d)", failed)
	}
	logger.Info("snmptrap: MIB catalogue ready",
		slog.Int("modules_loaded", loaded),
		slog.Int("modules_failed", failed),
		slog.Int("dirs", len(dirs)))
	return &gosmiResolver{cache: map[string]string{}, logger: logger}, nil
}

// Resolve performs the dotted-OID → MIB-qualified-name lookup. Cache hits
// short-circuit the gosmi call. The fallback (no MIB match) is the dotted
// OID with the leading "." stripped, matching NoopResolver's contract.
func (r *gosmiResolver) Resolve(oid string) string {
	if oid == "" {
		return ""
	}
	r.mu.RLock()
	cached, ok := r.cache[oid]
	r.mu.RUnlock()
	if ok {
		return cached
	}
	resolved := r.lookup(oid)
	r.mu.Lock()
	r.cache[oid] = resolved
	r.mu.Unlock()
	return resolved
}

// lookup turns a dotted OID into a MIB-qualified label, appending the index
// suffix that gosmi's RenderQualified strips. Mirrors the index-append loop
// in the old Python:
//
//	name = f"{module}::{symbol}"
//	for suffix in indices:
//	    name += f".{suffix}"
func (r *gosmiResolver) lookup(oid string) string {
	parsed, err := parseOID(oid)
	if err != nil || len(parsed) == 0 {
		return strings.TrimPrefix(oid, ".")
	}
	// Walk from the full length back to the root so the longest matching
	// MIB symbol wins. gosmi.GetNodeByOID returns an error when no module
	// covers the OID; we try shorter prefixes until something matches.
	for prefixLen := len(parsed); prefixLen > 0; prefixLen-- {
		node, err := gosmi.GetNodeByOID(parsed[:prefixLen])
		if err != nil {
			continue
		}
		qualified := node.RenderQualified()
		if qualified == "" {
			continue
		}
		// Append the trailing sub-identifiers (the table indices) so a
		// scalar like sysDescr.0 still renders as "SNMPv2-MIB::sysDescr.0"
		// and not just "SNMPv2-MIB::sysDescr".
		if prefixLen < len(parsed) {
			parts := make([]string, 0, len(parsed)-prefixLen)
			for _, sub := range parsed[prefixLen:] {
				parts = append(parts, strconv.FormatUint(uint64(sub), 10))
			}
			qualified += "." + strings.Join(parts, ".")
		}
		return qualified
	}
	return strings.TrimPrefix(oid, ".")
}

// parseOID splits a dotted-decimal OID string into its numeric sub-identifiers.
// Accepts both ".1.3.6..." and "1.3.6..." shapes. Empty / non-numeric segments
// fail the parse so the caller can fall back to the unresolved branch.
func parseOID(oid string) (gosmitypes.Oid, error) {
	trimmed := strings.TrimPrefix(oid, ".")
	if trimmed == "" {
		return nil, errors.New("empty oid")
	}
	parts := strings.Split(trimmed, ".")
	out := make(gosmitypes.Oid, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.ParseUint(p, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("bad sub-identifier %q: %w", p, err)
		}
		out = append(out, gosmitypes.SmiSubId(n))
	}
	return out, nil
}
