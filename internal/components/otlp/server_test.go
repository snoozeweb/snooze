package otlp

import (
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// writeFile is a tiny helper used by the config test.
func writeFile(path string, body []byte) error {
	return os.WriteFile(path, body, 0o600)
}

// fakePoster captures the records handed to PostAlerts. It is race-safe.
type fakePoster struct {
	mu      sync.Mutex
	batches [][]snoozetypes.Record
	err     error
}

func (f *fakePoster) PostAlerts(_ context.Context, recs []snoozetypes.Record) ([]snoozetypes.Record, []error, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := make([]snoozetypes.Record, len(recs))
	copy(cp, recs)
	f.batches = append(f.batches, cp)
	if f.err != nil {
		return nil, nil, f.err
	}
	return recs, nil, nil
}

func (f *fakePoster) all() []snoozetypes.Record {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []snoozetypes.Record
	for _, b := range f.batches {
		out = append(out, b...)
	}
	return out
}

func newTestServer(poster recordPoster) *server {
	s := newServer("127.0.0.1:0", poster, nil)
	// Pin "now" so timestamp-default assertions are deterministic.
	s.now = func() time.Time { return time.Date(2022, 1, 1, 0, 0, 0, 0, time.UTC) }
	return s
}

func doRequest(t *testing.T, s *server, req *http.Request) *http.Response {
	t.Helper()
	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, req)
	return rec.Result()
}

func TestHandleLogs_OKEmptyResponse(t *testing.T) {
	fp := &fakePoster{}
	s := newTestServer(fp)

	req := httptest.NewRequest(http.MethodPost, "/v1/logs", bytes.NewReader([]byte(sampleLogsJSON)))
	req.Header.Set("Content-Type", "application/json")
	resp := doRequest(t, s, req)
	defer resp.Body.Close() //nolint:errcheck

	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "application/json", resp.Header.Get("Content-Type"))
	body := readAll(t, resp)
	require.Equal(t, "{}", string(bytes.TrimSpace(body)))

	got := fp.all()
	require.Len(t, got, 1)
	require.Equal(t, "web-01", got[0].Host)
	require.Equal(t, "warning", got[0].Severity)
}

func TestHandleLogs_MalformedJSON_400(t *testing.T) {
	s := newTestServer(&fakePoster{})
	req := httptest.NewRequest(http.MethodPost, "/v1/logs", bytes.NewReader([]byte(`{not json`)))
	req.Header.Set("Content-Type", "application/json")
	resp := doRequest(t, s, req)
	defer resp.Body.Close() //nolint:errcheck
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestHandleLogs_ProtobufContentType_415(t *testing.T) {
	fp := &fakePoster{}
	s := newTestServer(fp)
	req := httptest.NewRequest(http.MethodPost, "/v1/logs", bytes.NewReader([]byte(sampleLogsJSON)))
	req.Header.Set("Content-Type", "application/x-protobuf")
	resp := doRequest(t, s, req)
	defer resp.Body.Close() //nolint:errcheck
	require.Equal(t, http.StatusUnsupportedMediaType, resp.StatusCode)
	require.Empty(t, fp.all()) // nothing forwarded
}

func TestHandleLogs_WrongMethod_405(t *testing.T) {
	s := newTestServer(&fakePoster{})
	req := httptest.NewRequest(http.MethodGet, "/v1/logs", nil)
	resp := doRequest(t, s, req)
	defer resp.Body.Close() //nolint:errcheck
	require.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
	require.Equal(t, http.MethodPost, resp.Header.Get("Allow"))
}

func TestHandleLogs_GzipBody(t *testing.T) {
	fp := &fakePoster{}
	s := newTestServer(fp)

	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	_, err := gw.Write([]byte(sampleLogsJSON))
	require.NoError(t, err)
	require.NoError(t, gw.Close())

	req := httptest.NewRequest(http.MethodPost, "/v1/logs", bytes.NewReader(buf.Bytes()))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "gzip")
	resp := doRequest(t, s, req)
	defer resp.Body.Close() //nolint:errcheck

	require.Equal(t, http.StatusOK, resp.StatusCode)
	got := fp.all()
	require.Len(t, got, 1)
	require.Equal(t, "disk usage at 92%", got[0].Message)
}

func TestHandleLogs_EmptyContentTypeTreatedAsJSON(t *testing.T) {
	fp := &fakePoster{}
	s := newTestServer(fp)
	req := httptest.NewRequest(http.MethodPost, "/v1/logs", bytes.NewReader([]byte(sampleLogsJSON)))
	// No Content-Type set.
	resp := doRequest(t, s, req)
	defer resp.Body.Close() //nolint:errcheck
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Len(t, fp.all(), 1)
}

func TestForward_PosterErrorIsNotFatal(t *testing.T) {
	fp := &fakePoster{err: context.DeadlineExceeded}
	s := newTestServer(fp)
	req := httptest.NewRequest(http.MethodPost, "/v1/logs", bytes.NewReader([]byte(sampleLogsJSON)))
	req.Header.Set("Content-Type", "application/json")
	resp := doRequest(t, s, req)
	defer resp.Body.Close() //nolint:errcheck
	// OTLP spec: request was accepted → still 200 even when forwarding failed.
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestForward_NoPosterDegrades(t *testing.T) {
	s := newTestServer(nil) // no client wired
	req := httptest.NewRequest(http.MethodPost, "/v1/logs", bytes.NewReader([]byte(sampleLogsJSON)))
	req.Header.Set("Content-Type", "application/json")
	resp := doRequest(t, s, req)
	defer resp.Body.Close() //nolint:errcheck
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestHandleMetrics_StubReturnsOK(t *testing.T) {
	fp := &fakePoster{}
	s := newTestServer(fp)
	req := httptest.NewRequest(http.MethodPost, "/v1/metrics",
		bytes.NewReader([]byte(`{"resourceMetrics":[]}`)))
	req.Header.Set("Content-Type", "application/json")
	resp := doRequest(t, s, req)
	defer resp.Body.Close() //nolint:errcheck
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "{}", string(bytes.TrimSpace(readAll(t, resp))))
	require.Empty(t, fp.all()) // metrics map to nothing
}

func TestHandleMetrics_ProtobufContentType_415(t *testing.T) {
	s := newTestServer(&fakePoster{})
	req := httptest.NewRequest(http.MethodPost, "/v1/metrics", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/x-protobuf")
	resp := doRequest(t, s, req)
	defer resp.Body.Close() //nolint:errcheck
	require.Equal(t, http.StatusUnsupportedMediaType, resp.StatusCode)
}

func readAll(t *testing.T, resp *http.Response) []byte {
	t.Helper()
	buf := new(bytes.Buffer)
	_, err := buf.ReadFrom(resp.Body)
	require.NoError(t, err)
	return buf.Bytes()
}
