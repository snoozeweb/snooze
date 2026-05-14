package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// dialUnix opens a one-shot HTTP-over-unix client targeting path.
func dialUnix(path string) *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", path)
			},
		},
	}
}

func TestAdminSocket_RootToken(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("admin socket peer-cred check requires linux")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "admin.sock")

	srv := &AdminServer{
		Path:        path,
		Tokens:      testTokenEngine(t),
		UID:         os.Getuid(),
		AllowedUIDs: []int{os.Getuid()},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = srv.ListenAndServe(ctx)
	}()

	// Wait for the socket to come up.
	require.Eventually(t, func() bool {
		_, err := os.Stat(path)
		return err == nil
	}, 2*time.Second, 20*time.Millisecond)

	client := dialUnix(path)
	resp, err := client.Get("http://unix/api/root_token")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var body struct {
		RootToken string `json:"root_token"`
	}
	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &body))
	require.NotEmpty(t, body.RootToken)

	cancel()
	wg.Wait()
}

func TestAdminSocket_RejectsUnknownPeer(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("admin socket peer-cred check requires linux")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "admin.sock")

	// Deliberately empty AllowedUIDs and UID=99999 → the test process uid
	// won't be in the allow set (root is always allowed, so skip if root).
	if os.Getuid() == 0 {
		t.Skip("running as root; cannot exercise rejection path")
	}
	srv := &AdminServer{
		Path:        path,
		Tokens:      testTokenEngine(t),
		UID:         99999,
		AllowedUIDs: nil,
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = srv.ListenAndServe(ctx)
	}()
	require.Eventually(t, func() bool {
		_, err := os.Stat(path)
		return err == nil
	}, 2*time.Second, 20*time.Millisecond)

	client := dialUnix(path)
	_, err := client.Get("http://unix/api/root_token")
	// Either the connection is closed before any reply (Get returns
	// EOF / connection reset) or returns a transport error. Anything but
	// a clean 200 satisfies us.
	require.Error(t, err)
	require.True(t, errors.Is(err, io.EOF) || err != nil)

	cancel()
	wg.Wait()
}

func TestAdminSocket_RequiresTokenEngine(t *testing.T) {
	dir := t.TempDir()
	srv := &AdminServer{Path: filepath.Join(dir, "x.sock")}
	err := srv.ListenAndServe(context.Background())
	require.Error(t, err)
}

func TestAdminSocket_EmptyPath(t *testing.T) {
	srv := &AdminServer{Tokens: testTokenEngine(t)}
	err := srv.ListenAndServe(context.Background())
	require.Error(t, err)
}
