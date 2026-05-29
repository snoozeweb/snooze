package snmptrap_test

import (
	"net"
	"strings"
	"testing"
	"time"

	"github.com/gosnmp/gosnmp"
	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/components/snmptrap"
)

// remoteAddr returns a fixed UDPAddr used across the parser tests so the Host
// assertion is stable.
func remoteAddr(t *testing.T) *net.UDPAddr {
	t.Helper()
	addr, err := net.ResolveUDPAddr("udp", "192.0.2.42:34567")
	require.NoError(t, err)
	return addr
}

// noResolver is the parser-test default: every test feeds in NoopResolver{}
// so the assertions exercise the sanitization path without depending on a
// MIB catalogue. Tests that need MIB resolution stub a fake resolver inline.
func noResolver() snmptrap.OIDResolver { return snmptrap.NoopResolver{} }

func TestParseTrap_V2c_BasicMapping(t *testing.T) {
	pkt := &gosnmp.SnmpPacket{
		Version:   gosnmp.Version2c,
		Community: "public",
		PDUType:   gosnmp.SNMPv2Trap,
		Variables: []gosnmp.SnmpPDU{
			{Name: ".1.3.6.1.2.1.1.3.0", Type: gosnmp.TimeTicks, Value: uint32(12345)},
			{Name: ".1.3.6.1.6.3.1.1.4.1.0", Type: gosnmp.ObjectIdentifier, Value: ".1.3.6.1.4.1.8072.2.3.0.1"},
			{Name: ".1.3.6.1.4.1.8072.2.3.2.1", Type: gosnmp.OctetString, Value: []byte("disk failing")},
		},
	}

	parsed := snmptrap.ParseTrap(pkt, remoteAddr(t), noResolver())

	require.Equal(t, "192.0.2.42", parsed.Host)
	require.Equal(t, "2c", parsed.Version)
	// Process must be the last component of the trap OID.
	require.Equal(t, "1", parsed.Process)
	// No severity varbind supplied → default warning.
	require.Equal(t, "warning", parsed.Severity)
	// Raw keys are sanitized: leading dot stripped, remaining dots → "_".
	// This is the Snooze 1.x behaviour from
	// components/snmptrap/src/snooze_snmptrap/main.py:_handler.
	require.Equal(t, "disk failing", parsed.Raw["1_3_6_1_4_1_8072_2_3_2_1"])
	require.Equal(t, int64(12345), parsed.Raw["1_3_6_1_2_1_1_3_0"])
	// Message follows the same key convention.
	require.Contains(t, parsed.Message, "1_3_6_1_4_1_8072_2_3_2_1=disk failing")
	require.Contains(t, parsed.Message, "1_3_6_1_6_3_1_1_4_1_0=.1.3.6.1.4.1.8072.2.3.0.1")

	rec := parsed.ToRecord(time.Unix(1700000000, 0).UTC())
	require.Equal(t, "snmptrap", rec.Source)
	require.Equal(t, "192.0.2.42", rec.Host)
	require.Equal(t, "1", rec.Process)
	require.Equal(t, "warning", rec.Severity)
	require.Equal(t, int64(1700000000), rec.DateEpoch)
	require.Equal(t, "2c", rec.Raw["snmp_version"])
}

func TestParseTrap_SeverityHeuristic(t *testing.T) {
	// The heuristic looks for "severity" or "priority" as a substring of the
	// (sanitized) varbind name. Pre-resolved MIB symbols like
	// "CISCO-SYSLOG-MIB::clogHistSeverity" pass through SanitizeKey unchanged
	// — the "::" delimiter and the keyword match survive.
	pkt := &gosnmp.SnmpPacket{
		Version: gosnmp.Version2c,
		PDUType: gosnmp.SNMPv2Trap,
		Variables: []gosnmp.SnmpPDU{
			{Name: ".1.3.6.1.6.3.1.1.4.1.0", Type: gosnmp.ObjectIdentifier, Value: ".1.3.6.1.4.1.9.9.41.2.0.1"},
			{Name: "CISCO-SYSLOG-MIB::clogHistSeverity", Type: gosnmp.OctetString, Value: []byte("CRITICAL")},
		},
	}

	parsed := snmptrap.ParseTrap(pkt, remoteAddr(t), noResolver())

	require.Equal(t, "critical", parsed.Severity, "severity should be normalised to lower-case canonical form")
}

func TestParseTrap_V1FallbackToEnterprise(t *testing.T) {
	pkt := &gosnmp.SnmpPacket{
		Version: gosnmp.Version1,
		PDUType: gosnmp.Trap,
		SnmpTrap: gosnmp.SnmpTrap{
			Enterprise:   ".1.3.6.1.4.1.2021.250.10",
			AgentAddress: "10.0.0.1",
			GenericTrap:  6,
			SpecificTrap: 1,
		},
		Variables: []gosnmp.SnmpPDU{
			{Name: ".1.3.6.1.4.1.2021.250.10.1", Type: gosnmp.OctetString, Value: []byte("v1 payload")},
		},
	}

	parsed := snmptrap.ParseTrap(pkt, remoteAddr(t), noResolver())

	require.Equal(t, "1", parsed.Version)
	// snmpTrapOID is absent in v1 → Process falls back to last component of
	// the Enterprise OID.
	require.Equal(t, "10", parsed.Process)
	require.Contains(t, parsed.Message, "1_3_6_1_4_1_2021_250_10_1=v1 payload")
}

func TestParseTrap_NilRemoteAndEmptyPacket(t *testing.T) {
	// nil remote → empty Host; nil packet → safe defaults with no varbinds.
	// nil resolver is also tolerated (falls back to NoopResolver internally).
	parsed := snmptrap.ParseTrap(nil, nil, nil)
	require.Empty(t, parsed.Host)
	require.Equal(t, "warning", parsed.Severity)
	require.Equal(t, "snmptrap", parsed.Process)
	require.Empty(t, parsed.Message)
	require.Empty(t, parsed.Raw)
}

func TestParseTrap_MessageOrderingDeterministic(t *testing.T) {
	// Build a packet whose varbinds, sorted alphabetically by sanitized key,
	// land in a stable order regardless of insertion sequence.
	pkt := &gosnmp.SnmpPacket{
		Version: gosnmp.Version2c,
		PDUType: gosnmp.SNMPv2Trap,
		Variables: []gosnmp.SnmpPDU{
			{Name: ".1.3.6.1.4.1.9", Type: gosnmp.OctetString, Value: []byte("z")},
			{Name: ".1.3.6.1.4.1.1", Type: gosnmp.OctetString, Value: []byte("a")},
			{Name: ".1.3.6.1.4.1.5", Type: gosnmp.OctetString, Value: []byte("m")},
		},
	}
	parsed := snmptrap.ParseTrap(pkt, remoteAddr(t), noResolver())
	require.True(t,
		strings.Index(parsed.Message, "1_3_6_1_4_1_1=") < strings.Index(parsed.Message, "1_3_6_1_4_1_5="),
		"varbinds should be sorted alphabetically: %s", parsed.Message,
	)
	require.True(t,
		strings.Index(parsed.Message, "1_3_6_1_4_1_5=") < strings.Index(parsed.Message, "1_3_6_1_4_1_9="),
		"varbinds should be sorted alphabetically: %s", parsed.Message,
	)
}

// fakeResolver implements OIDResolver with a literal map so MIB-resolution
// behaviour can be unit-tested without loading a real MIB tree. Unknown OIDs
// fall through to NoopResolver semantics.
type fakeResolver map[string]string

func (f fakeResolver) Resolve(oid string) string {
	if v, ok := f[oid]; ok {
		return v
	}
	return snmptrap.NoopResolver{}.Resolve(oid)
}

func TestParseTrap_ResolverRenamesKeys(t *testing.T) {
	// Verifies the full happy path: dotted OID → resolved name → sanitized
	// key in Raw. Mirrors the Python `_process_mib` + `record[key.replace(".", "_")]` chain.
	resolver := fakeResolver{
		".1.3.6.1.2.1.1.3.0":        "SNMPv2-MIB::sysUpTime.0",
		".1.3.6.1.6.3.1.1.4.1.0":    "SNMPv2-MIB::snmpTrapOID.0",
		".1.3.6.1.4.1.8072.2.3.2.1": "NET-SNMP-EXAMPLES-MIB::netSnmpExampleHeartbeatName",
	}
	pkt := &gosnmp.SnmpPacket{
		Version: gosnmp.Version2c,
		PDUType: gosnmp.SNMPv2Trap,
		Variables: []gosnmp.SnmpPDU{
			{Name: ".1.3.6.1.2.1.1.3.0", Type: gosnmp.TimeTicks, Value: uint32(12345)},
			{Name: ".1.3.6.1.6.3.1.1.4.1.0", Type: gosnmp.ObjectIdentifier, Value: ".1.3.6.1.4.1.8072.2.3.0.1"},
			{Name: ".1.3.6.1.4.1.8072.2.3.2.1", Type: gosnmp.OctetString, Value: []byte("disk failing")},
		},
	}

	parsed := snmptrap.ParseTrap(pkt, remoteAddr(t), resolver)

	// "::" survives SanitizeKey; "." in the trailing index becomes "_".
	require.Equal(t, int64(12345), parsed.Raw["SNMPv2-MIB::sysUpTime_0"])
	require.Equal(t, "disk failing", parsed.Raw["NET-SNMP-EXAMPLES-MIB::netSnmpExampleHeartbeatName"])
	// snmpTrapOID is still recognised by its dotted form, so Process still
	// derives from the trap OID value.
	require.Equal(t, "1", parsed.Process)
}

func TestSanitizeKey(t *testing.T) {
	t.Run("strips leading dot and underscores the rest", func(t *testing.T) {
		require.Equal(t, "1_3_6_1_2_1_1_3_0", snmptrap.SanitizeKey(".1.3.6.1.2.1.1.3.0"))
	})
	t.Run("preserves :: from MIB-qualified names", func(t *testing.T) {
		require.Equal(t, "SNMPv2-MIB::sysUpTime_0", snmptrap.SanitizeKey("SNMPv2-MIB::sysUpTime.0"))
	})
	t.Run("empty input is empty output", func(t *testing.T) {
		require.Equal(t, "", snmptrap.SanitizeKey(""))
	})
}

func TestNoopResolver_TrimsLeadingDot(t *testing.T) {
	r := snmptrap.NoopResolver{}
	require.Equal(t, "1.3.6.1.2.1.1.3.0", r.Resolve(".1.3.6.1.2.1.1.3.0"))
	require.Equal(t, "1.3.6.1.2.1.1.3.0", r.Resolve("1.3.6.1.2.1.1.3.0"))
	require.Equal(t, "", r.Resolve(""))
}
