package mattermost

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// wsURL converts an http(s):// Mattermost site URL into the matching ws(s)://
// endpoint for /api/v4/websocket. It is permissive about trailing slashes
// and explicit ports.
func wsURL(siteURL string) (string, error) {
	u, err := url.Parse(strings.TrimRight(siteURL, "/"))
	if err != nil {
		return "", fmt.Errorf("mattermost ws: parse site url: %w", err)
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	case "wss", "ws":
		// already a ws scheme — accept it.
	default:
		return "", fmt.Errorf("mattermost ws: unsupported scheme %q", u.Scheme)
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/api/v4/websocket"
	return u.String(), nil
}

// wsEvent is the Mattermost WebSocket envelope. We only decode the fields
// the daemon needs; everything else is left in Data as raw JSON to avoid
// brittle schema coupling.
type wsEvent struct {
	Event     string                     `json:"event"`
	Data      map[string]json.RawMessage `json:"data,omitempty"`
	Broadcast map[string]json.RawMessage `json:"broadcast,omitempty"`
	Seq       int                        `json:"seq"`
}

// wsAuthChallenge is the first frame the client sends after connect to
// authenticate the WebSocket session. Mattermost expects a v4
// authentication_challenge with the personal access token.
type wsAuthChallenge struct {
	Seq    int                    `json:"seq"`
	Action string                 `json:"action"`
	Data   map[string]interface{} `json:"data"`
}

// wsClient wraps a single gorilla/websocket connection. It is single-shot:
// once the connection closes, the caller (Daemon.run) should construct a
// fresh wsClient via dialWS.
type wsClient struct {
	conn   *websocket.Conn
	seq    atomic.Int64
	logger *slog.Logger
}

// dialWS opens a Mattermost WebSocket and sends the authentication challenge.
// The returned wsClient is ready to receive events via ReadEvent.
//
// `insecure` disables TLS verification for the WS handshake — keep it off
// unless you're testing against a self-signed dev instance.
func dialWS(ctx context.Context, siteURL, token string, insecure bool, logger *slog.Logger) (*wsClient, error) {
	target, err := wsURL(siteURL)
	if err != nil {
		return nil, err
	}
	dialer := *websocket.DefaultDialer
	dialer.HandshakeTimeout = 15 * time.Second
	if insecure {
		dialer.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // opt-in
	}
	conn, _, err := dialer.DialContext(ctx, target, http.Header{})
	if err != nil {
		return nil, fmt.Errorf("mattermost ws: dial %s: %w", target, err)
	}
	w := &wsClient{conn: conn, logger: logger}
	// authentication_challenge is the first frame Mattermost expects from
	// the client after a successful handshake. Failure closes the socket.
	w.seq.Store(1)
	challenge := wsAuthChallenge{
		Seq:    1,
		Action: "authentication_challenge",
		Data:   map[string]interface{}{"token": token},
	}
	if err := conn.WriteJSON(challenge); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("mattermost ws: send auth challenge: %w", err)
	}
	return w, nil
}

// Close shuts the underlying WebSocket. Safe to call once.
func (w *wsClient) Close() error {
	if w == nil || w.conn == nil {
		return nil
	}
	return w.conn.Close()
}

// ReadEvent blocks until the next JSON event arrives, the deadline expires
// or the connection closes. ReadEvent returns the parsed wsEvent or an error
// — including io.EOF-style websocket close errors which the caller should
// treat as a signal to reconnect.
func (w *wsClient) ReadEvent(ctx context.Context) (*wsEvent, error) {
	if w == nil || w.conn == nil {
		return nil, errors.New("mattermost ws: nil connection")
	}
	// Apply a generous read deadline so a half-open TCP connection eventually
	// surfaces as an error and triggers reconnect.
	_ = w.conn.SetReadDeadline(time.Now().Add(2 * time.Minute))
	type result struct {
		ev  *wsEvent
		err error
	}
	ch := make(chan result, 1)
	go func() {
		var ev wsEvent
		if err := w.conn.ReadJSON(&ev); err != nil {
			ch <- result{nil, err}
			return
		}
		ch <- result{&ev, nil}
	}()
	select {
	case <-ctx.Done():
		_ = w.conn.Close()
		return nil, ctx.Err()
	case r := <-ch:
		return r.ev, r.err
	}
}

// nextSeq returns the next monotonic sequence number for outbound frames.
// Mattermost requires `seq` on every action frame.
func (w *wsClient) nextSeq() int {
	return int(w.seq.Add(1))
}

// SendPing emits a `user_typing`-style ping action. We don't actually need
// the typing semantic — Mattermost's WebSocket has no required ping/pong
// from the client, but writing periodically keeps NAT mappings alive and
// surfaces dead connections faster than the read deadline alone.
func (w *wsClient) SendPing() error {
	if w == nil || w.conn == nil {
		return errors.New("mattermost ws: nil connection")
	}
	_ = w.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return w.conn.WriteJSON(map[string]any{
		"seq":    w.nextSeq(),
		"action": "ping",
		"data":   map[string]any{},
	})
}
