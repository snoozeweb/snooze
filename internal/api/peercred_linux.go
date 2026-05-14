//go:build linux

package api

import (
	"errors"
	"fmt"
	"net"

	"golang.org/x/sys/unix"
)

// peerCredListener wraps a net.Listener and rejects accepted connections
// whose peer uid is not in allowed. The check uses SO_PEERCRED (Linux).
type peerCredListener struct {
	net.Listener
	allowed map[int]struct{}
}

// Accept returns the next inbound connection after verifying its peer uid.
func (p *peerCredListener) Accept() (net.Conn, error) {
	conn, err := p.Listener.Accept()
	if err != nil {
		return nil, err
	}
	uc, ok := conn.(*net.UnixConn)
	if !ok {
		_ = conn.Close()
		return nil, errors.New("admin socket: non-unix peer")
	}
	uid, err := peerUID(uc)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("admin socket: peer uid: %w", err)
	}
	if _, ok := p.allowed[uid]; !ok {
		_ = conn.Close()
		return nil, fmt.Errorf("admin socket: peer uid %d not allowed", uid)
	}
	return conn, nil
}

// peerUID returns the uid of the process on the far end of conn via
// SO_PEERCRED. The result is read from the underlying syscall.Conn so the
// connection is not closed in the happy path.
func peerUID(uc *net.UnixConn) (int, error) {
	raw, err := uc.SyscallConn()
	if err != nil {
		return -1, err
	}
	var (
		ucred *unix.Ucred
		ferr  error
	)
	err = raw.Control(func(fd uintptr) {
		ucred, ferr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	})
	if err != nil {
		return -1, err
	}
	if ferr != nil {
		return -1, ferr
	}
	if ucred == nil {
		return -1, errors.New("nil ucred")
	}
	return int(ucred.Uid), nil
}
