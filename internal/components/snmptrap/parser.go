package snmptrap

import (
	"fmt"
	"net"
	"sort"
	"strings"
	"time"

	"github.com/gosnmp/gosnmp"

	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// snmpTrapOID is the dotted-OID for SNMPv2-MIB::snmpTrapOID.0. v2c/v3 traps
// carry the actual trap identifier as the value of this varbind.
const snmpTrapOID = ".1.3.6.1.6.3.1.1.4.1.0"

// severityKeywords matches varbind labels (case-insensitive substring) that
// likely carry a textual severity. The first hit wins. The match runs against
// the varbind Name string as supplied by gosnmp: pure dotted OIDs will never
// match, but senders that pre-resolve OIDs to MIB symbols (e.g.
// "CISCO-SYSLOG-MIB::clogHistSeverity") will be picked up. Without a MIB
// compiler on our side we accept this limitation; the default severity remains
// "warning" otherwise.
//
//nolint:gochecknoglobals // intentionally a package-level lookup table
var severityKeywords = []string{"severity", "priority"}

// knownSeverities normalises a freeform severity value to a Snooze-friendly
// token. Anything that doesn't match falls through to the raw string.
//
//nolint:gochecknoglobals // lookup table
var knownSeverities = map[string]string{
	"crit": "critical", "critical": "critical", "fatal": "critical", "emerg": "critical", "emergency": "critical",
	"err": "err", "error": "err",
	"warn": "warning", "warning": "warning",
	"notice": "notice",
	"info":   "info", "informational": "info",
	"debug": "debug",
	"ok":    "ok", "clear": "ok", "normal": "ok",
}

// ParsedTrap is the intermediate shape produced by ParseTrap and consumed by
// the forwarder. Keeping it separate from snoozetypes.Record means the parser
// stays testable in isolation.
type ParsedTrap struct {
	// Host is the dotted-IP of the sender (empty when unknown).
	Host string
	// Process is a short label for the trap (last OID component or trap OID).
	Process string
	// Severity is the heuristic-mapped severity token ("warning" by default).
	Severity string
	// Message is a "k=v k=v ..." rendering of the varbinds, sorted by key.
	Message string
	// Raw is the full OID→value map (values pretty-printed).
	Raw map[string]any
	// Version is "1", "2c" or "3" — useful for downstream tagging.
	Version string
}

// ParseTrap converts an inbound gosnmp packet into a ParsedTrap. The remote
// UDPAddr is consulted for the Host field; nil is accepted (sender unknown).
// The resolver is consulted to convert each dotted OID into a MIB-qualified
// label; pass NoopResolver{} to skip MIB lookups.
//
// The function never returns an error: it always produces a best-effort record
// so a malformed trap is still surfaced to the operator rather than silently
// dropped.
func ParseTrap(pkt *gosnmp.SnmpPacket, remote *net.UDPAddr, resolver OIDResolver) ParsedTrap {
	if resolver == nil {
		resolver = NoopResolver{}
	}
	out := ParsedTrap{
		Severity: "warning",
		Process:  "snmptrap",
		Raw:      map[string]any{},
		Version:  versionString(pkt),
	}
	if remote != nil {
		out.Host = remote.IP.String()
	}

	if pkt == nil {
		return out
	}

	var trapOID string
	for _, vb := range pkt.Variables {
		dotted := normaliseOID(vb.Name)
		value := pdoValue(vb)
		// Resolver gives us the MIB-qualified label when possible, the
		// bare dotted OID (sans leading dot) otherwise. SanitizeKey then
		// replaces "." with "_" so the key is safe to use as a BSON
		// sub-document field — see resolver.go for the rationale.
		key := SanitizeKey(resolver.Resolve(dotted))
		out.Raw[key] = value

		// Locate the trap OID per RFC 3416 — v2c/v3 stash it as the value of
		// snmpTrapOID.0. We check against the original dotted form because
		// the resolver may have rewritten the name into a MIB symbol.
		if trapOID == "" && (dotted == snmpTrapOID || strings.TrimSuffix(dotted, ".0") == strings.TrimSuffix(snmpTrapOID, ".0")) {
			if s, ok := value.(string); ok {
				trapOID = s
			}
		}

		// First hit on a severity-looking varbind wins.
		if out.Severity == "warning" {
			lower := strings.ToLower(key)
			for _, kw := range severityKeywords {
				if strings.Contains(lower, kw) {
					out.Severity = normaliseSeverity(stringify(value))
					break
				}
			}
		}
	}

	// v1 traps don't carry snmpTrapOID — fall back to the Enterprise OID.
	if trapOID == "" && pkt.Enterprise != "" {
		trapOID = pkt.Enterprise
	}
	if trapOID != "" {
		out.Process = processLabel(trapOID)
	}
	out.Message = renderVarbinds(out.Raw)

	return out
}

// versionString turns the gosnmp.SnmpVersion enum into the canonical wire token.
func versionString(pkt *gosnmp.SnmpPacket) string {
	if pkt == nil {
		return ""
	}
	switch pkt.Version {
	case gosnmp.Version1:
		return "1"
	case gosnmp.Version2c:
		return "2c"
	case gosnmp.Version3:
		return "3"
	default:
		return fmt.Sprintf("%d", pkt.Version)
	}
}

// normaliseOID returns the OID prefixed with a leading dot, matching the
// dotted-decimal form used by SNMP RFCs and by net-snmp. gosnmp emits both
// shapes depending on the packet path.
func normaliseOID(s string) string {
	if s == "" {
		return s
	}
	if !strings.HasPrefix(s, ".") {
		return "." + s
	}
	return s
}

// pdoValue extracts a Go-native value from a SnmpPDU. We special-case the
// common BER types so downstream consumers see strings/ints/bools rather than
// gosnmp internal wrappers. Time stamps are stored as their integer tick value
// (hundredths of a second) since wall-clock conversion isn't lossless here.
func pdoValue(vb gosnmp.SnmpPDU) any {
	switch vb.Type {
	case gosnmp.OctetString:
		if b, ok := vb.Value.([]byte); ok {
			return string(b)
		}
		return stringify(vb.Value)
	case gosnmp.ObjectIdentifier:
		return stringify(vb.Value)
	case gosnmp.IPAddress:
		return stringify(vb.Value)
	case gosnmp.Integer, gosnmp.Counter32, gosnmp.Gauge32, gosnmp.Uinteger32, gosnmp.TimeTicks:
		return toInt64(vb.Value)
	case gosnmp.Counter64:
		return toUint64(vb.Value)
	case gosnmp.Boolean:
		if b, ok := vb.Value.(bool); ok {
			return b
		}
	case gosnmp.Null, gosnmp.NoSuchObject, gosnmp.NoSuchInstance, gosnmp.EndOfMibView:
		return nil
	}
	return vb.Value
}

// stringify is a defensive any→string conversion that covers the common
// gosnmp value shapes ([]byte, fmt.Stringer, etc.).
func stringify(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case []byte:
		return string(t)
	case fmt.Stringer:
		return t.String()
	default:
		return fmt.Sprintf("%v", t)
	}
}

// toInt64 widens any numeric BER value into int64, falling back to 0 on
// unexpected types so the parser doesn't panic on malformed traps.
func toInt64(v any) int64 {
	switch n := v.(type) {
	case int:
		return int64(n)
	case int32:
		return int64(n)
	case int64:
		return n
	case uint:
		return int64(n) //nolint:gosec // value range bounded by SNMP encoding
	case uint32:
		return int64(n)
	case uint64:
		return int64(n) //nolint:gosec // value range bounded by SNMP encoding
	default:
		return 0
	}
}

// toUint64 widens unsigned-shaped values; mirror of toInt64 for Counter64.
func toUint64(v any) uint64 {
	switch n := v.(type) {
	case uint:
		return uint64(n)
	case uint32:
		return uint64(n)
	case uint64:
		return n
	case int:
		return uint64(n) //nolint:gosec // SNMP unsigned counters never carry sign
	case int64:
		return uint64(n) //nolint:gosec // SNMP unsigned counters never carry sign
	default:
		return 0
	}
}

// normaliseSeverity collapses upper-case / synonym severity tokens onto the
// canonical Snooze severity vocabulary. Unknown tokens are passed through
// lower-cased so operators see what they configured.
func normaliseSeverity(in string) string {
	in = strings.TrimSpace(strings.ToLower(in))
	if in == "" {
		return "warning"
	}
	if mapped, ok := knownSeverities[in]; ok {
		return mapped
	}
	return in
}

// processLabel picks the most useful short label for the trap. We prefer the
// last dotted component (typical net-snmp convention) and fall back to the
// full OID when no dots are present.
func processLabel(trapOID string) string {
	if trapOID == "" {
		return "snmptrap"
	}
	parts := strings.Split(strings.TrimPrefix(trapOID, "."), ".")
	if len(parts) == 0 {
		return trapOID
	}
	return parts[len(parts)-1]
}

// renderVarbinds joins the OID→value map into a stable "k=v k=v" string. We
// sort keys so the output is deterministic across runs (and across goroutines)
// — important because this string ends up in the alert payload and may be
// used as a fingerprint.
func renderVarbinds(varbinds map[string]any) string {
	if len(varbinds) == 0 {
		return ""
	}
	keys := make([]string, 0, len(varbinds))
	for k := range varbinds {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", k, stringify(varbinds[k])))
	}
	return strings.Join(parts, " ")
}

// ToRecord turns a ParsedTrap into the snoozetypes.Record wire shape. Caller
// supplies the wall-clock timestamp so tests can pin it.
func (p ParsedTrap) ToRecord(now time.Time) snoozetypes.Record {
	rec := snoozetypes.Record{
		Host:      p.Host,
		Source:    "snmptrap",
		Process:   p.Process,
		Severity:  p.Severity,
		Message:   p.Message,
		Timestamp: now,
		DateEpoch: now.Unix(),
		Raw:       cloneRaw(p.Raw),
	}
	if p.Version != "" {
		if rec.Raw == nil {
			rec.Raw = map[string]any{}
		}
		rec.Raw["snmp_version"] = p.Version
	}
	return rec
}

// cloneRaw returns a shallow copy of m so callers can mutate the parsed map
// without affecting future records. nil maps round-trip to nil.
func cloneRaw(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
