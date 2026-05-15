package relp

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestWriteFrameRoundTrip exercises the canonical encoding by writing every
// supported command shape and re-reading it through the framer.
func TestWriteFrameRoundTrip(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   Frame
		// onWire is the expected byte-for-byte serialisation. We assert it
		// explicitly so any drift in the wire format trips the test.
		onWire string
	}{
		{
			name:   "syslog frame",
			in:     Frame{TxnR: 1, Command: CmdSyslog, Data: []byte("<13>1 - host - - - hello")},
			onWire: "1 syslog 24 <13>1 - host - - - hello\n",
		},
		{
			name:   "open frame",
			in:     Frame{TxnR: 1, Command: CmdOpen, Data: []byte("relp_version=0\nrelp_software=rsyslog")},
			onWire: "1 open 36 relp_version=0\nrelp_software=rsyslog\n",
		},
		{
			name:   "ack frame",
			in:     AckFrame(42),
			onWire: "42 rsp 6 200 OK\n",
		},
		{
			name:   "empty data frame uses no separator before LF",
			in:     ServerCloseFrame(),
			onWire: "0 serverclose 0\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			require.NoError(t, WriteFrame(&buf, tc.in))
			require.Equal(t, tc.onWire, buf.String(), "wire format drifted")

			fr := NewFrameReader(bytes.NewReader(buf.Bytes()), 1<<20)
			got, err := fr.ReadFrame()
			require.NoError(t, err)
			require.Equal(t, tc.in.TxnR, got.TxnR)
			require.Equal(t, tc.in.Command, got.Command)
			require.Equal(t, string(tc.in.Data), string(got.Data))
		})
	}
}

// TestReadFrameStream feeds several concatenated frames and asserts they
// decode in order. This is the framing model RELP uses in practice — one TCP
// connection carries many transactions.
func TestReadFrameStream(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	frames := []Frame{
		{TxnR: 1, Command: CmdOpen, Data: []byte("relp_version=0")},
		{TxnR: 2, Command: CmdSyslog, Data: []byte("<13>1 - host - - - one")},
		{TxnR: 3, Command: CmdSyslog, Data: []byte("<13>1 - host - - - two")},
		{TxnR: 4, Command: CmdClose},
	}
	for _, f := range frames {
		require.NoError(t, WriteFrame(&buf, f))
	}
	fr := NewFrameReader(&buf, 1<<20)
	for i, want := range frames {
		got, err := fr.ReadFrame()
		require.NoErrorf(t, err, "frame %d", i)
		require.Equal(t, want.TxnR, got.TxnR)
		require.Equal(t, want.Command, got.Command)
		require.Equal(t, string(want.Data), string(got.Data))
	}
	// After the last frame the stream is exhausted.
	_, err := fr.ReadFrame()
	require.ErrorIs(t, err, io.EOF)
}

// TestReadFrameRejectsOversize ensures the maxLen cap is honoured: an
// announced DATALEN larger than the cap must be rejected without reading the
// data block.
func TestReadFrameRejectsOversize(t *testing.T) {
	t.Parallel()
	// 1000 bytes of data, but we cap at 100.
	payload := strings.Repeat("x", 1000)
	wire := "1 syslog 1000 " + payload + "\n"
	fr := NewFrameReader(strings.NewReader(wire), 100)
	_, err := fr.ReadFrame()
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrFrameTooLarge), "got %v", err)
}

// TestReadFrameMalformed sweeps a handful of invalid byte sequences to
// confirm the parser rejects them with a descriptive error rather than
// crashing or hanging.
func TestReadFrameMalformed(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		wire string
	}{
		{"non-digit txnr", "X syslog 0\n"},
		{"missing command", "1 \n"},
		{"missing datalen", "1 syslog X\n"},
		{"unexpected EOF mid-data", "1 syslog 10 abc"},
		{"missing trailer", "1 syslog 3 abcD"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fr := NewFrameReader(strings.NewReader(tc.wire), 1<<20)
			_, err := fr.ReadFrame()
			require.Error(t, err)
		})
	}
}

// TestParseOpenOffers verifies the helper that exposes the negotiation
// payload structure. We only consume the data on the dispatch path, but
// the helper documents what a well-formed offer block looks like.
func TestParseOpenOffers(t *testing.T) {
	t.Parallel()
	got := parseOpenOffers([]byte("relp_version=0\nrelp_software=rsyslog 8.2310.0\ncommands=syslog\n"))
	require.Equal(t, "0", got["relp_version"])
	require.Equal(t, "rsyslog 8.2310.0", got["relp_software"])
	require.Equal(t, "syslog", got["commands"])
}
