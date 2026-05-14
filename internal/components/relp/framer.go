package relp

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
)

// RELP wire format (librelp / rsyslog):
//
//	FRAME    = HEADER DATA TRAILER
//	HEADER   = TXNR SP COMMAND SP DATALEN
//	          [ SP DATA ]            ; SP is omitted when DATALEN == 0
//	TRAILER  = LF                    ; ASCII 0x0A, single byte
//
//	TXNR     = positive decimal integer, < 10 digits in practice
//	COMMAND  = ASCII token, e.g. "open", "syslog", "rsp", "close",
//	           "serverclose", "abort"
//	DATALEN  = decimal integer giving the length of DATA in bytes
//	DATA     = opaque payload of exactly DATALEN bytes
//
// Reference: https://www.rsyslog.com/doc/relp.html and librelp's
// `relpframe.c`. We implement just enough commands to receive syslog frames
// and ACK them; see the package doc for which extensions are skipped.

// Command names defined by the RELP spec. Constants keep callers honest
// about what we implement.
const (
	CmdOpen        = "open"
	CmdSyslog      = "syslog"
	CmdClose       = "close"
	CmdServerClose = "serverclose"
	CmdRsp         = "rsp"
	CmdAbort       = "abort"
)

// Frame is a single decoded RELP frame.
type Frame struct {
	// TxnR is the transaction number assigned by the sender. The server is
	// required to ACK every frame with the same TxnR.
	TxnR uint64

	// Command is one of the CmdXxx constants above.
	Command string

	// Data is the opaque payload (already stripped of its DATALEN prefix and
	// trailing LF). Empty when DATALEN == 0.
	Data []byte
}

// ErrFrameTooLarge is returned by ReadFrame when a peer announces a DATALEN
// larger than the configured cap. The connection should be dropped.
var ErrFrameTooLarge = errors.New("relp: frame exceeds max size")

// FrameReader decodes RELP frames from an io.Reader. It is NOT safe for
// concurrent use — RELP sessions are strictly serial per connection.
type FrameReader struct {
	br     *bufio.Reader
	maxLen int
}

// NewFrameReader wraps r. maxLen caps the data section to defend against
// hostile peers; pass 0 to disable the cap (not recommended on a public
// listener).
func NewFrameReader(r io.Reader, maxLen int) *FrameReader {
	br, ok := r.(*bufio.Reader)
	if !ok {
		br = bufio.NewReaderSize(r, 64<<10)
	}
	return &FrameReader{br: br, maxLen: maxLen}
}

// ReadFrame decodes the next RELP frame from the underlying stream.
//
// It returns io.EOF cleanly when the peer closes the connection between
// frames. A partial frame (EOF mid-stream) is reported as io.ErrUnexpectedEOF
// wrapped in a descriptive error.
func (fr *FrameReader) ReadFrame() (Frame, error) {
	var f Frame

	// TXNR field: digits up to the first space.
	txnr, err := fr.readUintField(' ')
	if err != nil {
		return f, err
	}
	f.TxnR = txnr

	// COMMAND field: ASCII letters up to the next space.
	cmd, err := fr.readTokenField(' ')
	if err != nil {
		return f, fmt.Errorf("relp: read command: %w", err)
	}
	f.Command = cmd

	// DATALEN: digits terminated by either SP (when DATA follows) or LF
	// (when DATALEN == 0). We must peek to disambiguate.
	datalen, sep, err := fr.readUintFieldAny(' ', '\n')
	if err != nil {
		return f, fmt.Errorf("relp: read datalen: %w", err)
	}
	if fr.maxLen > 0 && datalen > uint64(fr.maxLen) {
		return f, ErrFrameTooLarge
	}

	if datalen == 0 {
		// Per spec, when DATALEN==0 there is no SP and the frame already
		// ended on the LF we just consumed.
		if sep != '\n' {
			// Some senders still emit "<txnr> <cmd> 0 \n"; tolerate that by
			// requiring the trailing LF here.
			if err := fr.expectByte('\n'); err != nil {
				return f, err
			}
		}
		return f, nil
	}
	if sep != ' ' {
		return f, fmt.Errorf("relp: missing SP before DATA for txnr=%d cmd=%s", f.TxnR, f.Command)
	}

	data := make([]byte, datalen)
	if _, err := io.ReadFull(fr.br, data); err != nil {
		if errors.Is(err, io.EOF) {
			err = io.ErrUnexpectedEOF
		}
		return f, fmt.Errorf("relp: read data: %w", err)
	}
	f.Data = data

	// TRAILER: single LF. librelp tolerates clients that omit it before the
	// next frame's header, but the spec is explicit so we require it.
	if err := fr.expectByte('\n'); err != nil {
		return f, err
	}
	return f, nil
}

// readUintField reads ASCII decimal digits up to (and consuming) the
// terminator byte. It returns io.EOF when the stream ends before any byte is
// read — the FrameReader uses this to detect clean shutdowns.
func (fr *FrameReader) readUintField(term byte) (uint64, error) {
	var buf [20]byte
	n := 0
	for {
		b, err := fr.br.ReadByte()
		if err != nil {
			if errors.Is(err, io.EOF) && n == 0 {
				return 0, io.EOF
			}
			if errors.Is(err, io.EOF) {
				return 0, io.ErrUnexpectedEOF
			}
			return 0, err
		}
		if b == term {
			if n == 0 {
				return 0, fmt.Errorf("relp: empty numeric field")
			}
			v, err := strconv.ParseUint(string(buf[:n]), 10, 64)
			if err != nil {
				return 0, fmt.Errorf("relp: invalid number %q: %w", buf[:n], err)
			}
			return v, nil
		}
		if b < '0' || b > '9' {
			return 0, fmt.Errorf("relp: invalid byte %q in numeric field", b)
		}
		if n >= len(buf) {
			return 0, fmt.Errorf("relp: numeric field too long")
		}
		buf[n] = b
		n++
	}
}

// readUintFieldAny is like readUintField but accepts either of two
// terminators; it returns which terminator was actually consumed so the
// caller can disambiguate (DATALEN can be followed by SP or LF).
func (fr *FrameReader) readUintFieldAny(t1, t2 byte) (uint64, byte, error) {
	var buf [20]byte
	n := 0
	for {
		b, err := fr.br.ReadByte()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return 0, 0, io.ErrUnexpectedEOF
			}
			return 0, 0, err
		}
		if b == t1 || b == t2 {
			if n == 0 {
				return 0, 0, fmt.Errorf("relp: empty numeric field")
			}
			v, err := strconv.ParseUint(string(buf[:n]), 10, 64)
			if err != nil {
				return 0, 0, fmt.Errorf("relp: invalid number %q: %w", buf[:n], err)
			}
			return v, b, nil
		}
		if b < '0' || b > '9' {
			return 0, 0, fmt.Errorf("relp: invalid byte %q in numeric field", b)
		}
		if n >= len(buf) {
			return 0, 0, fmt.Errorf("relp: numeric field too long")
		}
		buf[n] = b
		n++
	}
}

// readTokenField reads a printable ASCII token up to (and consuming) the
// terminator byte.
func (fr *FrameReader) readTokenField(term byte) (string, error) {
	var buf [32]byte
	n := 0
	for {
		b, err := fr.br.ReadByte()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return "", io.ErrUnexpectedEOF
			}
			return "", err
		}
		if b == term {
			if n == 0 {
				return "", fmt.Errorf("relp: empty command field")
			}
			return string(buf[:n]), nil
		}
		if b < 0x21 || b > 0x7e {
			return "", fmt.Errorf("relp: invalid byte %q in command field", b)
		}
		if n >= len(buf) {
			return "", fmt.Errorf("relp: command field too long")
		}
		buf[n] = b
		n++
	}
}

// expectByte reads exactly one byte and errors if it is not want.
func (fr *FrameReader) expectByte(want byte) error {
	got, err := fr.br.ReadByte()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return io.ErrUnexpectedEOF
		}
		return err
	}
	if got != want {
		return fmt.Errorf("relp: expected byte %q got %q", want, got)
	}
	return nil
}

// WriteFrame serialises f to w. It is the inverse of ReadFrame and is used
// by the server to emit rsp ACKs and serverclose frames.
func WriteFrame(w io.Writer, f Frame) error {
	header := strconv.AppendUint(nil, f.TxnR, 10)
	header = append(header, ' ')
	header = append(header, f.Command...)
	header = append(header, ' ')
	header = strconv.AppendInt(header, int64(len(f.Data)), 10)
	if len(f.Data) > 0 {
		header = append(header, ' ')
	}
	if _, err := w.Write(header); err != nil {
		return err
	}
	if len(f.Data) > 0 {
		if _, err := w.Write(f.Data); err != nil {
			return err
		}
	}
	if _, err := w.Write([]byte{'\n'}); err != nil {
		return err
	}
	return nil
}

// AckFrame builds a `rsp 6 200 OK` frame ACKing txnr. The librelp convention
// is "<status> <text>" inside DATA where 200 means success.
func AckFrame(txnr uint64) Frame {
	return Frame{TxnR: txnr, Command: CmdRsp, Data: []byte("200 OK")}
}

// NackFrame builds a "rsp 500 <reason>" frame. Used when a downstream POST
// fails — the client is then expected to retry the same txnr.
func NackFrame(txnr uint64, reason string) Frame {
	if reason == "" {
		reason = "internal error"
	}
	data := append([]byte("500 "), reason...)
	return Frame{TxnR: txnr, Command: CmdRsp, Data: data}
}

// ServerCloseFrame is the frame the server emits before closing the TCP
// connection cleanly. RELP requires txnr=0 for unsolicited server frames.
func ServerCloseFrame() Frame {
	return Frame{TxnR: 0, Command: CmdServerClose}
}
