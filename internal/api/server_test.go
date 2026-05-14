package api

import (
	"context"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNewServer_DefaultsBasePath(t *testing.T) {
	s := NewServer(Config{}, http.NotFoundHandler(), nil)
	require.NotNil(t, s)
	require.NotNil(t, s.Server)
}

// TestServer_ListenAndServeUnix exercises the unix listener and the graceful
// shutdown path by sending exactly one request.
func TestServer_ListenAndServeUnix(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "http.sock")
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	srv := NewServer(Config{
		UnixSocket:  path,
		GracePeriod: time.Second,
	}, handler, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = srv.ListenAndServe(ctx)
	}()

	require.Eventually(t, func() bool {
		c, err := net.Dial("unix", path)
		if err != nil {
			return false
		}
		_ = c.Close()
		return true
	}, 2*time.Second, 20*time.Millisecond)

	client := &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", path)
		},
	}}
	resp, err := client.Get("http://unix/")
	require.NoError(t, err)
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	require.Equal(t, "ok", string(body))

	cancel()
	wg.Wait()
}

func TestServer_TCPGracefulShutdown(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	_ = ln.Close()

	srv := NewServer(Config{Addr: addr, GracePeriod: time.Second},
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }), nil)

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); _ = srv.ListenAndServe(ctx) }()

	require.Eventually(t, func() bool {
		c, err := net.Dial("tcp", addr)
		if err != nil {
			return false
		}
		_ = c.Close()
		return true
	}, 2*time.Second, 20*time.Millisecond)

	cancel()
	wg.Wait()
}
