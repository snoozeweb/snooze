//go:build !linux

package api

import (
	"errors"
	"net"
)

// peerCredListener on non-Linux platforms rejects every accept: the admin
// socket peer-cred check is implemented via SO_PEERCRED and the Linux-only
// golang.org/x/sys/unix Ucred path. The rest of Snooze targets Linux for
// production, but the unit test suite must still compile on darwin/windows.
type peerCredListener struct {
	net.Listener
	allowed map[int]struct{}
}

// Accept always errors on platforms without SO_PEERCRED. Returning an error
// rather than silently letting the connection through forces a deployer to
// notice that the admin socket is unsafe on their platform.
func (p *peerCredListener) Accept() (net.Conn, error) {
	c, err := p.Listener.Accept()
	if err != nil {
		return nil, err
	}
	_ = c.Close()
	return nil, errors.New("admin socket: SO_PEERCRED unsupported on this platform")
}
