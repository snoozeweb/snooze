package pacemaker_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/japannext/snooze/internal/components/pacemaker"
	"github.com/japannext/snooze/pkg/snoozetypes"
)

// newSnoozeStub stands in for snooze-server. It accepts the v1 login + alerts
// endpoints and lets callers inspect the most-recent posted record.
type snoozeStub struct {
	srv       *httptest.Server
	loginHits atomic.Int32
	alertHits atomic.Int32
	lastRec   snoozetypes.Record
}

func newSnoozeStub(t *testing.T) *snoozeStub {
	t.Helper()
	s := &snoozeStub{}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/login/local", func(w http.ResponseWriter, _ *http.Request) {
		s.loginHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"token":  "tok-1",
			"method": "local",
		})
	})
	mux.HandleFunc("/api/v1/alerts", func(w http.ResponseWriter, r *http.Request) {
		s.alertHits.Add(1)
		require.Equal(t, http.MethodPost, r.Method)
		var rec snoozetypes.Record
		require.NoError(t, json.NewDecoder(r.Body).Decode(&rec))
		s.lastRec = rec
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{
				"uid":     "u-1",
				"host":    rec.Host,
				"source":  rec.Source,
				"process": rec.Process,
			}},
		})
	})
	s.srv = httptest.NewServer(mux)
	t.Cleanup(s.srv.Close)
	return s
}

// baseEnv returns an env map pointing the runner at stub. Tests mutate it to
// adjust action / nodename / etc. A per-test token-cache file keeps test runs
// hermetic and -race friendly.
func baseEnv(t *testing.T, stub *snoozeStub) map[string]string {
	t.Helper()
	return map[string]string{
		"SNOOZE_SERVER":           stub.srv.URL,
		"SNOOZE_USERNAME":         "alice",
		"SNOOZE_PASSWORD":         "hunter2",
		"SNOOZE_TOKEN_CACHE_FILE": filepath.Join(t.TempDir(), "token"),
	}
}

func TestRunner_FenceActionPostsRecord(t *testing.T) {
	stub := newSnoozeStub(t)
	env := baseEnv(t, stub)
	env["nodename"] = "db-01"
	env["reason"] = "node unresponsive"

	var stdout, stderr bytes.Buffer
	r := pacemaker.NewRunner(pacemaker.Options{
		Env:    env,
		Stdin:  strings.NewReader(""),
		Stdout: &stdout,
		Stderr: &stderr,
	})

	code, err := r.Run(context.Background(), []string{"reboot"})
	require.NoError(t, err)
	require.Equal(t, 0, code, "stderr=%s", stderr.String())
	require.Equal(t, int32(1), stub.alertHits.Load(), "expected exactly one alert POST")
	require.Equal(t, "db-01", stub.lastRec.Host)
	require.Equal(t, "pacemaker", stub.lastRec.Source)
	require.Equal(t, "fence", stub.lastRec.Process)
	require.Equal(t, "critical", stub.lastRec.Severity)
	require.Equal(t, "node unresponsive", stub.lastRec.Message)
	require.ElementsMatch(t, []string{"fence", "cluster"}, stub.lastRec.Tags)
	require.Equal(t, "reboot", stub.lastRec.Raw["action"])
}

func TestRunner_PassiveActionsSkipNetwork(t *testing.T) {
	stub := newSnoozeStub(t)

	for _, action := range []string{"status", "monitor", "list", "validate-all"} {
		t.Run(action, func(t *testing.T) {
			env := baseEnv(t, stub)
			env["nodename"] = "db-01"

			var stdout, stderr bytes.Buffer
			r := pacemaker.NewRunner(pacemaker.Options{
				Env:    env,
				Stdin:  strings.NewReader(""),
				Stdout: &stdout,
				Stderr: &stderr,
			})

			before := stub.alertHits.Load()
			code, err := r.Run(context.Background(), []string{action})
			require.NoError(t, err)
			require.Equal(t, 0, code, "stderr=%s", stderr.String())
			require.Equal(t, before, stub.alertHits.Load(), "%s must not POST", action)
		})
	}
}

func TestRunner_MetadataEmitsXML(t *testing.T) {
	stub := newSnoozeStub(t)

	var stdout, stderr bytes.Buffer
	r := pacemaker.NewRunner(pacemaker.Options{
		Env:    baseEnv(t, stub),
		Stdin:  strings.NewReader(""),
		Stdout: &stdout,
		Stderr: &stderr,
	})

	code, err := r.Run(context.Background(), []string{"metadata"})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Equal(t, int32(0), stub.alertHits.Load(), "metadata must not POST")
	require.Contains(t, stdout.String(), `<resource-agent name="snooze-pacemaker"`)
	require.Contains(t, stdout.String(), `<action name="reboot"`)
}

func TestRunner_MissingServerExitsOne(t *testing.T) {
	var stdout, stderr bytes.Buffer
	r := pacemaker.NewRunner(pacemaker.Options{
		Env:    map[string]string{"nodename": "db-01"}, // no SNOOZE_SERVER
		Stdin:  strings.NewReader(""),
		Stdout: &stdout,
		Stderr: &stderr,
	})

	code, err := r.Run(context.Background(), []string{"off"})
	require.Error(t, err)
	require.Equal(t, 1, code)
	require.Contains(t, stderr.String(), "SNOOZE_SERVER")
}

func TestRunner_UnknownActionExitsTwo(t *testing.T) {
	stub := newSnoozeStub(t)
	var stdout, stderr bytes.Buffer
	r := pacemaker.NewRunner(pacemaker.Options{
		Env:    baseEnv(t, stub),
		Stdin:  strings.NewReader(""),
		Stdout: &stdout,
		Stderr: &stderr,
	})

	code, err := r.Run(context.Background(), []string{"obliterate"})
	require.Error(t, err)
	require.Equal(t, 2, code)
	require.Contains(t, stderr.String(), "unknown action")
	require.Equal(t, int32(0), stub.alertHits.Load())
}

func TestRunner_StdinParamsAndPositionalHost(t *testing.T) {
	stub := newSnoozeStub(t)
	// Server credentials come from env, nodename from stdin, action+host from args.
	env := baseEnv(t, stub)

	stdin := strings.NewReader("# pacemaker passes parameters this way\nreason=split-brain\n")
	var stdout, stderr bytes.Buffer
	r := pacemaker.NewRunner(pacemaker.Options{
		Env:    env,
		Stdin:  stdin,
		Stdout: &stdout,
		Stderr: &stderr,
	})

	code, err := r.Run(context.Background(), []string{"off", "web-02"})
	require.NoError(t, err)
	require.Equal(t, 0, code, "stderr=%s", stderr.String())
	require.Equal(t, "web-02", stub.lastRec.Host)
	require.Equal(t, "split-brain", stub.lastRec.Message)
	require.Equal(t, int32(1), stub.alertHits.Load())
}

func TestLoadConfig_MissingFileIsNotAnError(t *testing.T) {
	dir := t.TempDir()
	cfg, err := pacemaker.LoadConfig(filepath.Join(dir, "absent.yaml"))
	require.NoError(t, err)
	require.Equal(t, pacemaker.Config{}, cfg)
}

func TestLoadConfig_ParsesYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pacemaker.yaml")
	body := []byte("server: https://snooze.example.com\nusername: bob\npassword: s3cret\ninsecure: true\n")
	require.NoError(t, os.WriteFile(path, body, 0o600))
	cfg, err := pacemaker.LoadConfig(path)
	require.NoError(t, err)
	require.Equal(t, "https://snooze.example.com", cfg.Server)
	require.Equal(t, "bob", cfg.Username)
	require.Equal(t, "s3cret", cfg.Password)
	require.True(t, cfg.Insecure)
}
