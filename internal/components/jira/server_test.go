package jira

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestServer_handleAlert_singleObject(t *testing.T) {
	jira := newTestJira(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"key": "OPS-1"})
	})
	cfg, _ := minimalCfg().WithDefaults()
	f := newForwarder(cfg, jira, nil)
	srv := newHTTPServer("127.0.0.1:0", f, nil)

	body := []byte(`{"project_key":"OPS","alert":{"hash":"h1","host":"s","severity":"warning","message":"x"}}`)
	req := httptest.NewRequest(http.MethodPost, "/alert?snooze_action_name=jira", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	srv.handleAlert(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var out map[string]map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	require.Equal(t, "OPS-1", out["h1"]["issue_key"])
}

func TestServer_handleAlert_batchArray(t *testing.T) {
	jira := newTestJira(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"key": "OPS-2"})
	})
	cfg, _ := minimalCfg().WithDefaults()
	f := newForwarder(cfg, jira, nil)
	srv := newHTTPServer("127.0.0.1:0", f, nil)

	body := []byte(`[
		{"project_key":"OPS","alert":{"hash":"a"}},
		{"project_key":"OPS","alert":{"hash":"b"}}
	]`)
	req := httptest.NewRequest(http.MethodPost, "/alert", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	srv.handleAlert(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var out map[string]map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	require.Contains(t, out, "a")
	require.Contains(t, out, "b")
}

func TestServer_handleAlert_rejectsNonPost(t *testing.T) {
	srv := newHTTPServer("127.0.0.1:0", nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/alert", nil)
	rec := httptest.NewRecorder()
	srv.handleAlert(rec, req)
	require.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestServer_handleAlert_rejectsEmptyBody(t *testing.T) {
	cfg, _ := minimalCfg().WithDefaults()
	jira := newTestJira(t, func(w http.ResponseWriter, r *http.Request) {})
	f := newForwarder(cfg, jira, nil)
	srv := newHTTPServer("127.0.0.1:0", f, nil)
	req := httptest.NewRequest(http.MethodPost, "/alert", strings.NewReader(""))
	rec := httptest.NewRecorder()
	srv.handleAlert(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

// integration smoke test: bind to :0, fire a real HTTP request, expect 200.
func TestServer_run_integration(t *testing.T) {
	var jiraHits int
	jira := newTestJira(t, func(w http.ResponseWriter, r *http.Request) {
		jiraHits++
		_ = json.NewEncoder(w).Encode(map[string]any{"key": "OPS-42"})
	})
	cfg, _ := minimalCfg().WithDefaults()
	f := newForwarder(cfg, jira, nil)
	srv := newHTTPServer("127.0.0.1:0", f, nil)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Run(ctx) }()

	addr := srv.Addr()
	require.NotEmpty(t, addr)

	body := bytes.NewReader([]byte(`{"project_key":"OPS","alert":{"hash":"h"}}`))
	resp, err := http.Post("http://"+addr+"/alert", "application/json", body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	cancel()
	require.NoError(t, <-errCh)
	require.Equal(t, 1, jiraHits)
}
