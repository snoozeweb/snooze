package snmptrap_test

import (
	"net"
	"strings"
	"testing"
	"time"

	"github.com/gosnmp/gosnmp"
	"github.com/stretchr/testify/require"

	"github.com/japannext/snooze/internal/components/snmptrap"
)

// remoteAddr returns a fixed UDPAddr used across the parser tests so the Host
// assertion is stable.
func remoteAddr(t *testing.T) *net.UDPAddr {
	t.Helper()
	addr, err := net.ResolveUDPAddr("udp", "192.0.2.42:34567")
	require.NoError(t, err)
	return addr
}

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

	parsed := snmptrap.ParseTrap(pkt, remoteAddr(t))

	require.Equal(t, "192.0.2.42", parsed.Host)
	require.Equal(t, "2c", parsed.Version)
	// Process must be the last component of the trap OID.
	require.Equal(t, "1", parsed.Process)
	// No severity varbind supplied → default warning.
	require.Equal(t, "warning", parsed.Severity)
	// Message contains the deterministic k=v rendering.
	require.Contains(t, parsed.Message, ".1.3.6.1.4.1.8072.2.3.2.1=disk failing")
	require.Contains(t, parsed.Message, ".1.3.6.1.6.3.1.1.4.1.0=.1.3.6.1.4.1.8072.2.3.0.1")
	// Raw map carries every varbind (typed where possible).
	require.Equal(t, "disk failing", parsed.Raw[".1.3.6.1.4.1.8072.2.3.2.1"])
	require.Equal(t, int64(12345), parsed.Raw[".1.3.6.1.2.1.1.3.0"])

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
	// varbind name. In practice this works for senders that pre-resolve OIDs
	// to MIB symbols (e.g. CISCO-SYSLOG-MIB::clogHistSeverity) — pure dotted
	// OIDs won't match. Tests therefore use a symbolic-style name.
	pkt := &gosnmp.SnmpPacket{
		Version: gosnmp.Version2c,
		PDUType: gosnmp.SNMPv2Trap,
		Variables: []gosnmp.SnmpPDU{
			{Name: ".1.3.6.1.6.3.1.1.4.1.0", Type: gosnmp.ObjectIdentifier, Value: ".1.3.6.1.4.1.9.9.41.2.0.1"},
			{Name: "CISCO-SYSLOG-MIB::clogHistSeverity", Type: gosnmp.OctetString, Value: []byte("CRITICAL")},
		},
	}

	parsed := snmptrap.ParseTrap(pkt, remoteAddr(t))

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

	parsed := snmptrap.ParseTrap(pkt, remoteAddr(t))

	require.Equal(t, "1", parsed.Version)
	// snmpTrapOID is absent in v1 → Process falls back to last component of
	// the Enterprise OID.
	require.Equal(t, "10", parsed.Process)
	require.Contains(t, parsed.Message, ".1.3.6.1.4.1.2021.250.10.1=v1 payload")
}

func TestParseTrap_NilRemoteAndEmptyPacket(t *testing.T) {
	// nil remote → empty Host; nil packet → safe defaults with no varbinds.
	parsed := snmptrap.ParseTrap(nil, nil)
	require.Empty(t, parsed.Host)
	require.Equal(t, "warning", parsed.Severity)
	require.Equal(t, "snmptrap", parsed.Process)
	require.Empty(t, parsed.Message)
	require.Empty(t, parsed.Raw)
}

func TestParseTrap_MessageOrderingDeterministic(t *testing.T) {
	// Build a packet whose varbinds, sorted alphabetically by OID, would land
	// in a different order than insertion. The rendered message must follow
	// the sorted order regardless of insertion sequence.
	pkt := &gosnmp.SnmpPacket{
		Version: gosnmp.Version2c,
		PDUType: gosnmp.SNMPv2Trap,
		Variables: []gosnmp.SnmpPDU{
			{Name: ".1.3.6.1.4.1.9", Type: gosnmp.OctetString, Value: []byte("z")},
			{Name: ".1.3.6.1.4.1.1", Type: gosnmp.OctetString, Value: []byte("a")},
			{Name: ".1.3.6.1.4.1.5", Type: gosnmp.OctetString, Value: []byte("m")},
		},
	}
	parsed := snmptrap.ParseTrap(pkt, remoteAddr(t))
	require.True(t,
		strings.Index(parsed.Message, ".1.3.6.1.4.1.1=") < strings.Index(parsed.Message, ".1.3.6.1.4.1.5="),
		"varbinds should be sorted alphabetically: %s", parsed.Message,
	)
	require.True(t,
		strings.Index(parsed.Message, ".1.3.6.1.4.1.5=") < strings.Index(parsed.Message, ".1.3.6.1.4.1.9="),
		"varbinds should be sorted alphabetically: %s", parsed.Message,
	)
}
