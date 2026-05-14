package snmptrap

import (
	"context"
	"errors"
	"io"
	"log"
	"log/slog"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/gosnmp/gosnmp"
	"github.com/stretchr/testify/require"

	"github.com/japannext/snooze/pkg/snoozetypes"
)

// This file lives in `package snmptrap` (not `_test`) so it can reach
// newDaemonWithDeps without widening the public API.

// stubPoster is an in-memory alertPoster used to assert that the daemon
// forwards the parsed trap exactly once with the expected fields.
type stubPoster struct {
	mu      sync.Mutex
	gotCh   chan snoozetypes.Record
	records []snoozetypes.Record
	err     error
}

func newStubPoster(buf int) *stubPoster {
	return &stubPoster{gotCh: make(chan snoozetypes.Record, buf)}
}

func (s *stubPoster) PostAlert(_ context.Context, rec snoozetypes.Record) (snoozetypes.Record, error) {
	s.mu.Lock()
	s.records = append(s.records, rec)
	err := s.err
	s.mu.Unlock()
	if err != nil {
		return snoozetypes.Record{}, err
	}
	// Non-blocking publish; the buffered channel covers the test's expected
	// fan-in. Drop on the floor if the consumer has gone away.
	select {
	case s.gotCh <- rec:
	default:
	}
	return rec, nil
}

// quietLogger returns a logger that swallows all output — useful so the test
// suite output stays clean while the daemon spits debug/info events.
func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

// ephemeralUDP grabs a free UDP port on loopback by binding briefly and
// closing the socket. There's an inherent race between the close and the
// listener bind, but on loopback during a single test process it's reliable
// enough — and certainly better than hard-coding 162.
func ephemeralUDP(t *testing.T) string {
	t.Helper()
	c, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	require.NoError(t, err)
	addr := c.LocalAddr().(*net.UDPAddr)
	require.NoError(t, c.Close())
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(addr.Port))
}

func TestDaemon_EndToEndV2c(t *testing.T) {
	listen := ephemeralUDP(t)
	host, portStr, err := net.SplitHostPort(listen)
	require.NoError(t, err)
	port, err := strconv.Atoi(portStr)
	require.NoError(t, err)

	cfg := Config{
		Server:    "https://snooze.example",
		Listen:    listen,
		Community: "public",
		Method:    "local",
		Timeout:   2 * time.Second,
	}

	poster := newStubPoster(4)
	d := newDaemonWithDeps(cfg, quietLogger(), poster)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runDone := make(chan error, 1)
	go func() { runDone <- d.Run(ctx) }()

	// Wait for the listener to be live before sending. We poll on Listening()
	// via the underlying TrapListener after Run installs it; the daemon
	// exposes no direct hook, so a short sleep is the simplest viable signal.
	require.Eventually(t, func() bool {
		// Probe: try to dial UDP. Until the listener binds, ConnectUDP will
		// happily succeed (UDP is connectionless) but the trap will be lost.
		// So instead we wait a bounded amount of time and rely on the
		// dispatcher loop polling-eventually for the recorded alert below.
		return true
	}, 250*time.Millisecond, 25*time.Millisecond)
	time.Sleep(100 * time.Millisecond) // small grace for bind

	// Build a v2c trap and send it to our listener.
	client := &gosnmp.GoSNMP{
		Target:    host,
		Port:      uint16(port), //nolint:gosec // ephemeral port fits in uint16
		Version:   gosnmp.Version2c,
		Community: "public",
		Timeout:   time.Second,
		Retries:   1,
		Logger:    gosnmp.NewLogger(log.New(io.Discard, "", 0)),
	}
	require.NoError(t, client.Connect())
	t.Cleanup(func() { _ = client.Conn.Close() })

	// Note: gosnmp's marshaller only accepts pure dotted-numeric OIDs on the
	// wire, so we can't test the severity heuristic end-to-end here (the
	// heuristic matches on symbolic names — see parser_test.go).
	trap := gosnmp.SnmpTrap{
		Variables: []gosnmp.SnmpPDU{
			{Name: ".1.3.6.1.2.1.1.3.0", Type: gosnmp.TimeTicks, Value: uint32(1)},
			{Name: ".1.3.6.1.6.3.1.1.4.1.0", Type: gosnmp.ObjectIdentifier, Value: ".1.3.6.1.4.1.8072.2.3.0.1"},
			{Name: ".1.3.6.1.4.1.8072.2.3.2.1", Type: gosnmp.OctetString, Value: "hello"},
		},
	}
	_, err = client.SendTrap(trap)
	require.NoError(t, err)

	// Wait for the forwarder to publish into our stub.
	select {
	case rec := <-poster.gotCh:
		require.Equal(t, "snmptrap", rec.Source)
		require.Equal(t, "1", rec.Process, "process should be last component of trap OID")
		require.Equal(t, "warning", rec.Severity, "default severity when no severity varbind is present")
		require.Equal(t, "127.0.0.1", rec.Host)
		require.Equal(t, "2c", rec.Raw["snmp_version"])
		require.Contains(t, rec.Message, "hello")
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for the daemon to forward the trap")
	}

	// Initiate shutdown and wait for Run to return cleanly.
	cancel()
	select {
	case err := <-runDone:
		// context.Canceled is the expected exit code on graceful shutdown.
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("Run returned unexpected error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return after context cancel")
	}
}

func TestDaemon_CommunityRejection(t *testing.T) {
	listen := ephemeralUDP(t)
	host, portStr, err := net.SplitHostPort(listen)
	require.NoError(t, err)
	port, err := strconv.Atoi(portStr)
	require.NoError(t, err)

	cfg := Config{
		Server:    "https://snooze.example",
		Listen:    listen,
		Community: "expected-secret",
		Method:    "local",
		Timeout:   2 * time.Second,
	}
	poster := newStubPoster(1)
	d := newDaemonWithDeps(cfg, quietLogger(), poster)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runDone := make(chan error, 1)
	go func() { runDone <- d.Run(ctx) }()
	time.Sleep(100 * time.Millisecond)

	// Send with wrong community: the listener must drop the trap silently.
	client := &gosnmp.GoSNMP{
		Target:    host,
		Port:      uint16(port), //nolint:gosec // ephemeral port fits in uint16
		Version:   gosnmp.Version2c,
		Community: "wrong",
		Timeout:   time.Second,
		Retries:   1,
		Logger:    gosnmp.NewLogger(log.New(io.Discard, "", 0)),
	}
	require.NoError(t, client.Connect())
	t.Cleanup(func() { _ = client.Conn.Close() })

	_, err = client.SendTrap(gosnmp.SnmpTrap{
		Variables: []gosnmp.SnmpPDU{
			{Name: ".1.3.6.1.6.3.1.1.4.1.0", Type: gosnmp.ObjectIdentifier, Value: ".1.3.6.1.4.1.99"},
		},
	})
	require.NoError(t, err)

	// We expect nothing to land in the poster channel within a short window.
	select {
	case rec := <-poster.gotCh:
		t.Fatalf("listener accepted wrong-community trap: %+v", rec)
	case <-time.After(250 * time.Millisecond):
		// Good — nothing forwarded.
	}

	cancel()
	<-runDone
}
