package cli

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

const peopleJSON = `{"data":[` +
	`{"name":"alice","method":"ldap","display_name":"Alice A.","avatar_version":"v1"},` +
	`{"name":"bob","method":"local","display_name":"","avatar_version":""},` +
	`{"name":"sam","method":"local"},{"name":"sam","method":"oidc"}]}`

func tinyPNG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 4, 4))))
	return buf.Bytes()
}

func TestPeople(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v1/people", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(peopleJSON))
	}))
	defer srv.Close()
	rt, _, _ := newTestRuntime(t, srv)
	rt.flags.Token = "tok"

	out, _, err := executeCmd(t, rt, "people")
	require.NoError(t, err)
	require.Regexp(t, `alice\s+ldap\s+Alice A\.\s+yes`, out)
	require.Regexp(t, `bob\s+local\s+no`, out)
}

func TestAvatarSetUploadsDataURL(t *testing.T) {
	raw := tinyPNG(t)
	file := filepath.Join(t.TempDir(), "me.png")
	require.NoError(t, os.WriteFile(file, raw, 0o600))

	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPut, r.Method)
		require.Equal(t, "/api/v1/user/me/avatar", r.URL.Path)
		require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"version":"abc123"}`))
	}))
	defer srv.Close()
	rt, _, _ := newTestRuntime(t, srv)
	rt.flags.Token = "tok"

	out, _, err := executeCmd(t, rt, "avatar", "set", file)
	require.NoError(t, err)
	require.Equal(t, "data:image/png;base64,"+base64.StdEncoding.EncodeToString(raw), got["data"])
	require.Contains(t, out, "version abc123")
}

// The content decides the type, whatever the extension says, and the size cap
// applies before anything is read or sent.
func TestAvatarSetRejectsLocally(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		t.Fatalf("server must not be called, got %s %s", r.Method, r.URL.Path)
	}))
	defer srv.Close()
	dir := t.TempDir()
	svg := filepath.Join(dir, "evil.png")
	require.NoError(t, os.WriteFile(svg, []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`), 0o600))
	big := filepath.Join(dir, "big.png")
	require.NoError(t, os.WriteFile(big, append(tinyPNG(t), make([]byte, maxAvatarFileBytes)...), 0o600))

	for name, tc := range map[string]struct{ file, want string }{
		"not an image": {svg, "must be PNG or JPEG"},
		"too big":      {big, "limited to 512 KiB"},
		"missing":      {filepath.Join(dir, "nope.png"), "no such file"},
	} {
		t.Run(name, func(t *testing.T) {
			rt, _, _ := newTestRuntime(t, srv)
			rt.flags.Token = "tok"
			_, _, err := executeCmd(t, rt, "avatar", "set", tc.file)
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestAvatarGetResolvesMethodAndSaves(t *testing.T) {
	raw := tinyPNG(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/people":
			_, _ = w.Write([]byte(peopleJSON))
		case "/api/v1/avatar/ldap/alice":
			_, _ = w.Write([]byte(`{"name":"alice","method":"ldap","version":"v1","data":"data:image/png;base64,` +
				base64.StdEncoding.EncodeToString(raw) + `"}`))
		default:
			t.Fatalf("unexpected %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	rt, _, _ := newTestRuntime(t, srv)
	rt.flags.Token = "tok"

	out, _, err := executeCmd(t, rt, "avatar", "get", "alice")
	require.NoError(t, err)
	require.Contains(t, out, "alice (ldap): version v1")

	file := filepath.Join(t.TempDir(), "alice.png")
	out, _, err = executeCmd(t, rt, "avatar", "get", "alice", "-o", file)
	require.NoError(t, err)
	require.Contains(t, out, "Saved alice's profile picture")
	saved, err := os.ReadFile(file)
	require.NoError(t, err)
	require.Equal(t, raw, saved)
}

func TestAvatarGetErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/people":
			_, _ = w.Write([]byte(peopleJSON))
		case "/api/v1/avatar/local/bob":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"code":"not_found","message":"no avatar"}}`))
		default:
			t.Fatalf("unexpected %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	for name, tc := range map[string]struct {
		args []string
		want string
	}{
		"no picture": {[]string{"bob"}, "bob has no profile picture"},
		"unknown":    {[]string{"zed"}, `no user "zed"`},
		"ambiguous":  {[]string{"sam"}, "pass --user-method"},
	} {
		t.Run(name, func(t *testing.T) {
			rt, _, _ := newTestRuntime(t, srv)
			rt.flags.Token = "tok"
			_, _, err := executeCmd(t, rt, append([]string{"avatar", "get"}, tc.args...)...)
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestAvatarRemove(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodDelete, r.Method)
		require.Equal(t, "/api/v1/user/me/avatar", r.URL.Path)
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	rt, _, _ := newTestRuntime(t, srv)
	rt.flags.Token = "tok"

	out, _, err := executeCmd(t, rt, "avatar", "remove")
	require.NoError(t, err)
	require.True(t, called)
	require.Contains(t, out, "Profile picture removed")
}
