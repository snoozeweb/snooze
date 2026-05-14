package relp

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestSessionOpenSyslogClose drives a full RELP session against the
// Listener: open → syslog → close. We assert the listener emits an ACK for
// every transaction and a serverclose after the peer's close.
func TestSessionOpenSyslogClose(t *testing.T) {
	t.Parallel()

	var (
		received [][]byte
		mu       sync.Mutex
	)
	handler := func(_ context.Context, payload []byte, _ string) error {
		mu.Lock()
		defer mu.Unlock()
		// Copy: payload is reused across frames.
		buf := make([]byte, len(payload))
		copy(buf, payload)
		received = append(received, buf)
		return nil
	}

	l, err := NewListener(ListenerOptions{
		Addr:        "127.0.0.1:0",
		Handler:     handler,
		MaxFrameLen: 1 << 20,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serveErr := make(chan error, 1)
	go func() { serveErr <- l.Serve(ctx) }()

	// Wait for the listener to bind. Addr() returns "" before that.
	require.Eventually(t, func() bool { return l.Addr() != "" },
		2*time.Second, 10*time.Millisecond, "listener never bound")

	conn, err := net.Dial("tcp", l.Addr())
	require.NoError(t, err)
	defer conn.Close()

	// Drive the session.
	require.NoError(t, WriteFrame(conn, Frame{TxnR: 1, Command: CmdOpen,
		Data: []byte("relp_version=0\nrelp_software=test-client\ncommands=syslog")}))
	require.NoError(t, WriteFrame(conn, Frame{TxnR: 2, Command: CmdSyslog,
		Data: []byte("<13>1 2024-01-02T03:04:05Z host app 1 - - hello world")}))
	require.NoError(t, WriteFrame(conn, Frame{TxnR: 3, Command: CmdSyslog,
		Data: []byte("<14>Jan  2 03:04:05 host app: legacy line")}))
	require.NoError(t, WriteFrame(conn, Frame{TxnR: 4, Command: CmdClose}))

	// Read four ACK/responses plus a serverclose.
	fr := NewFrameReader(conn, 1<<20)
	var responses []Frame
	for i := 0; i < 5; i++ {
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		f, err := fr.ReadFrame()
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		responses = append(responses, f)
		if f.Command == CmdServerClose {
			break
		}
	}
	require.GreaterOrEqual(t, len(responses), 5, "expected 5 responses, got %d", len(responses))

	// Open response: rsp txnr=1 starting with "200 OK".
	require.Equal(t, uint64(1), responses[0].TxnR)
	require.Equal(t, CmdRsp, responses[0].Command)
	require.True(t, bytes.HasPrefix(responses[0].Data, []byte("200 OK")),
		"open response should start with 200 OK: %q", responses[0].Data)
	require.Contains(t, string(responses[0].Data), "relp_software=snooze-relp")

	// Syslog ACKs.
	require.Equal(t, uint64(2), responses[1].TxnR)
	require.Equal(t, CmdRsp, responses[1].Command)
	require.Equal(t, "200 OK", string(responses[1].Data))
	require.Equal(t, uint64(3), responses[2].TxnR)
	require.Equal(t, "200 OK", string(responses[2].Data))

	// Close ACK.
	require.Equal(t, uint64(4), responses[3].TxnR)
	require.Equal(t, CmdRsp, responses[3].Command)

	// Server-initiated serverclose.
	require.Equal(t, CmdServerClose, responses[4].Command)

	// Both syslog frames reached the handler.
	mu.Lock()
	require.Len(t, received, 2)
	require.Contains(t, string(received[0]), "hello world")
	require.Contains(t, string(received[1]), "legacy line")
	mu.Unlock()

	cancel()
	select {
	case err := <-serveErr:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("listener did not stop")
	}
}

// TestSessionHandlerErrorEmitsNack ensures a handler returning a non-nil
// error produces a `rsp 500 ...` frame instead of a 200 OK ACK.
func TestSessionHandlerErrorEmitsNack(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	handler := func(_ context.Context, _ []byte, _ string) error {
		calls.Add(1)
		return errors.New("downstream offline")
	}

	l, err := NewListener(ListenerOptions{
		Addr:    "127.0.0.1:0",
		Handler: handler,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = l.Serve(ctx) }()
	require.Eventually(t, func() bool { return l.Addr() != "" },
		2*time.Second, 10*time.Millisecond)

	conn, err := net.Dial("tcp", l.Addr())
	require.NoError(t, err)
	defer conn.Close()

	require.NoError(t, WriteFrame(conn, Frame{TxnR: 7, Command: CmdSyslog,
		Data: []byte("<13>1 - - - - - boom")}))
	fr := NewFrameReader(conn, 1<<20)
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, err := fr.ReadFrame()
	require.NoError(t, err)
	require.Equal(t, uint64(7), resp.TxnR)
	require.Equal(t, CmdRsp, resp.Command)
	require.True(t, strings.HasPrefix(string(resp.Data), "500 "),
		"nack should start with 500: %q", resp.Data)
	require.Equal(t, int32(1), calls.Load())
}
