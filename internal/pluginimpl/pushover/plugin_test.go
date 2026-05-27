package pushover

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// ---- helpers ----------------------------------------------------------------

func sampleRecord() snoozetypes.Record {
	return snoozetypes.Record{
		UID:      "rec-1",
		Host:     "db-1.example.com",
		Source:   "syslog",
		Severity: "warning",
		Message:  "disk almost full",
	}
}

// newPluginForTest builds a Plugin with newClient overridden to the stdlib
// default (no TLS required for httptest plain servers).
func newPluginForTest(t *testing.T) *Plugin {
	t.Helper()
	p, err := factory(plugins.Metadata{Name: "pushover"})
	require.NoError(t, err)
	pp := p.(*Plugin)
	pp.newClient = func(timeout time.Duration) *http.Client {
		return &http.Client{Timeout: timeout}
	}
	return pp
}

// baseMeta returns the minimal action_form map that satisfies required fields,
// pointing at the provided server URL.
func baseMeta(serverURL string) map[string]any {
	return map[string]any{
		"token":    "apptoken123",
		"user":     "userkey456",
		"api_base": serverURL,
	}
}

// parseForm reads and parses the request body as application/x-www-form-urlencoded.
func parseForm(t *testing.T, r *http.Request) url.Values {
	t.Helper()
	body, err := io.ReadAll(r.Body)
	require.NoError(t, err)
	vals, err := url.ParseQuery(string(body))
	require.NoError(t, err)
	return vals
}

// ---- registration & contract ------------------------------------------------

func TestRegistration(t *testing.T) {
	require.True(t, slices.Contains(plugins.Registered(), "pushover"))
}

func TestPluginInterfaceContract(t *testing.T) {
	// Compile-time assertion; also exercise runtime methods.
	var _ plugins.Notifier = (*Plugin)(nil)
	p, err := factory(plugins.Metadata{Name: "pushover"})
	require.NoError(t, err)
	require.Equal(t, "pushover", p.Name())
	require.NoError(t, p.PostInit(context.Background(), nil))
	require.NoError(t, p.Reload(context.Background()))
}

// ---- happy-path send --------------------------------------------------------

// TestSendHappyPath verifies that a successful send posts the correct
// form-encoded fields (token, user, message, title, priority) and returns nil
// when the server responds with status=1.
func TestSendHappyPath(t *testing.T) {
	// Goroutine-safe capture; the server handler runs in a different goroutine.
	var (
		mu      sync.Mutex
		gotForm url.Values
		gotCT   string
		gotPath string
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f := parseForm(t, r)
		mu.Lock()
		gotForm = f
		gotCT = r.Header.Get("Content-Type")
		gotPath = r.URL.Path
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":1,"request":"fake-uuid"}`))
	}))
	t.Cleanup(srv.Close)

	p := newPluginForTest(t)
	rec := sampleRecord()
	err := p.Send(context.Background(), rec, plugins.NotificationPayload{
		Meta: map[string]any{
			"token":    "apptoken123",
			"user":     "userkey456",
			"api_base": srv.URL,
			"title":    "Alert: {{ .Host }}",
			"message":  "{{ .Message }}",
			"priority": "0",
		},
	})
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, "/1/messages.json", gotPath)
	require.Contains(t, gotCT, "application/x-www-form-urlencoded")
	require.Equal(t, "apptoken123", gotForm.Get("token"))
	require.Equal(t, "userkey456", gotForm.Get("user"))
	require.Equal(t, "Alert: db-1.example.com", gotForm.Get("title"))
	require.Equal(t, "disk almost full", gotForm.Get("message"))
	require.Equal(t, "0", gotForm.Get("priority"))
}

// TestSendAutoSeverityMapping verifies the severity→priority "auto" mapping
// for each Snooze severity keyword.
func TestSendAutoSeverityMapping(t *testing.T) {
	cases := []struct {
		severity     string
		wantPriority string
	}{
		{"emergency", "2"},
		{"critical", "2"},
		{"error", "1"},
		{"err", "1"},
		{"warning", "0"},
		{"warn", "0"},
		{"info", "-1"},
		{"notice", "-1"},
		{"debug", "-1"},
		{"", "-1"},
	}

	for _, tc := range cases {
		t.Run(tc.severity, func(t *testing.T) {
			var (
				mu      sync.Mutex
				gotPrio string
			)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				f := parseForm(t, r)
				mu.Lock()
				gotPrio = f.Get("priority")
				mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"status":1}`))
			}))
			t.Cleanup(srv.Close)

			p := newPluginForTest(t)
			rec := sampleRecord()
			rec.Severity = tc.severity
			meta := baseMeta(srv.URL)
			meta["priority"] = "auto"
			require.NoError(t, p.Send(context.Background(), rec, plugins.NotificationPayload{Meta: meta}))

			mu.Lock()
			defer mu.Unlock()
			require.Equal(t, tc.wantPriority, gotPrio, "severity=%q", tc.severity)
		})
	}
}

// TestSendEmergencyAddsRetryExpire ensures that priority 2 (emergency)
// automatically adds retry=60 and expire=3600 to the request body.
func TestSendEmergencyAddsRetryExpire(t *testing.T) {
	var (
		mu      sync.Mutex
		gotForm url.Values
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f := parseForm(t, r)
		mu.Lock()
		gotForm = f
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":1}`))
	}))
	t.Cleanup(srv.Close)

	p := newPluginForTest(t)
	meta := baseMeta(srv.URL)
	meta["priority"] = "2"
	require.NoError(t, p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{Meta: meta}))

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, "2", gotForm.Get("priority"))
	require.Equal(t, "60", gotForm.Get("retry"))
	require.Equal(t, "3600", gotForm.Get("expire"))
}

// TestSendNonEmergencyNoRetryExpire confirms that retry/expire are absent for
// priorities other than 2.
func TestSendNonEmergencyNoRetryExpire(t *testing.T) {
	var (
		mu      sync.Mutex
		gotForm url.Values
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f := parseForm(t, r)
		mu.Lock()
		gotForm = f
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":1}`))
	}))
	t.Cleanup(srv.Close)

	p := newPluginForTest(t)
	meta := baseMeta(srv.URL)
	meta["priority"] = "1"
	require.NoError(t, p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{Meta: meta}))

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, "1", gotForm.Get("priority"))
	require.Empty(t, gotForm.Get("retry"), "retry must be absent for non-emergency priority")
	require.Empty(t, gotForm.Get("expire"), "expire must be absent for non-emergency priority")
}

// TestSendOptionalFields verifies that sound, url, and url_title are only
// included when explicitly set.
func TestSendOptionalFields(t *testing.T) {
	var (
		mu      sync.Mutex
		gotForm url.Values
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f := parseForm(t, r)
		mu.Lock()
		gotForm = f
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":1}`))
	}))
	t.Cleanup(srv.Close)

	p := newPluginForTest(t)
	meta := baseMeta(srv.URL)
	meta["sound"] = "alien"
	meta["url"] = "https://grafana.example.com/dashboard"
	meta["url_title"] = "Open dashboard"
	meta["priority"] = "0"
	require.NoError(t, p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{Meta: meta}))

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, "alien", gotForm.Get("sound"))
	require.Equal(t, "https://grafana.example.com/dashboard", gotForm.Get("url"))
	require.Equal(t, "Open dashboard", gotForm.Get("url_title"))
}

// TestSendOptionalFieldsAbsent confirms that optional fields are absent from
// the wire when not configured.
func TestSendOptionalFieldsAbsent(t *testing.T) {
	var (
		mu      sync.Mutex
		gotForm url.Values
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f := parseForm(t, r)
		mu.Lock()
		gotForm = f
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":1}`))
	}))
	t.Cleanup(srv.Close)

	p := newPluginForTest(t)
	require.NoError(t, p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{Meta: baseMeta(srv.URL)}))

	mu.Lock()
	defer mu.Unlock()
	require.Empty(t, gotForm.Get("sound"))
	require.Empty(t, gotForm.Get("url"))
	require.Empty(t, gotForm.Get("url_title"))
}

// ---- error paths ------------------------------------------------------------

// TestSendAPIStatusError verifies that a status!=1 API response is surfaced as
// an error, including the "errors" array when present.
func TestSendAPIStatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":0,"errors":["invalid token","user not found"]}`))
	}))
	t.Cleanup(srv.Close)

	p := newPluginForTest(t)
	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{Meta: baseMeta(srv.URL)})
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid token")
	require.Contains(t, err.Error(), "user not found")
}

// TestSendAPIStatusErrorNoMessages covers status!=1 with an empty errors array.
func TestSendAPIStatusErrorNoMessages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":0}`))
	}))
	t.Cleanup(srv.Close)

	p := newPluginForTest(t)
	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{Meta: baseMeta(srv.URL)})
	require.Error(t, err)
	require.Contains(t, err.Error(), "status 0")
}

// TestSendHTTPError verifies that a non-200 HTTP status is returned as an error.
func TestSendHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)

	p := newPluginForTest(t)
	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{Meta: baseMeta(srv.URL)})
	require.Error(t, err)
	require.Contains(t, err.Error(), "401")
}

// TestSendMissingToken ensures that missing token yields a descriptive error
// before any HTTP request is made.
func TestSendMissingToken(t *testing.T) {
	p := newPluginForTest(t)
	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta: map[string]any{
			"user": "userkey456",
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "token")
}

// TestSendMissingUser ensures that missing user key yields a descriptive error.
func TestSendMissingUser(t *testing.T) {
	p := newPluginForTest(t)
	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta: map[string]any{
			"token": "apptoken123",
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "user")
}

// TestSendNilMeta covers the case where the NotificationPayload.Meta map is nil.
func TestSendNilMeta(t *testing.T) {
	p := newPluginForTest(t)
	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "token")
}

// TestSendInvalidPriority verifies that an out-of-range explicit priority is
// caught before any HTTP request is made.
func TestSendInvalidPriority(t *testing.T) {
	p := newPluginForTest(t)
	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta: map[string]any{
			"token":    "t",
			"user":     "u",
			"priority": "99",
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "priority")
}

// ---- default-template rendering ---------------------------------------------

// TestSendDefaultTemplates verifies that the default title and message
// templates render correctly when no explicit template is provided.
func TestSendDefaultTemplates(t *testing.T) {
	var (
		mu      sync.Mutex
		gotForm url.Values
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f := parseForm(t, r)
		mu.Lock()
		gotForm = f
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":1}`))
	}))
	t.Cleanup(srv.Close)

	p := newPluginForTest(t)
	rec := sampleRecord()
	require.NoError(t, p.Send(context.Background(), rec, plugins.NotificationPayload{Meta: baseMeta(srv.URL)}))

	mu.Lock()
	defer mu.Unlock()
	// Default title: "{{ .Severity }} on {{ .Host }}"
	require.Equal(t, "warning on db-1.example.com", gotForm.Get("title"))
	// Default message: "{{ .Message }}"
	require.Equal(t, "disk almost full", gotForm.Get("message"))
}

// ---- race safety ------------------------------------------------------------

// TestSendRaceSafety fires several concurrent Send calls at the same plugin
// instance. go test -race must not report data races.
func TestSendRaceSafety(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":1}`))
	}))
	t.Cleanup(srv.Close)

	p := newPluginForTest(t)
	meta := baseMeta(srv.URL)
	const workers = 10
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			_ = p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{Meta: meta})
		}()
	}
	wg.Wait()
}

// ---- resolvePriority unit tests ---------------------------------------------

func TestResolvePriorityAuto(t *testing.T) {
	cases := []struct {
		sev  string
		want int
	}{
		{"emergency", 2}, {"critical", 2},
		{"error", 1}, {"err", 1},
		{"warning", 0}, {"warn", 0},
		{"info", -1}, {"notice", -1}, {"debug", -1}, {"", -1},
	}
	for _, tc := range cases {
		got, err := resolvePriority("auto", tc.sev)
		require.NoError(t, err)
		require.Equal(t, tc.want, got, "sev=%q", tc.sev)
	}
}

func TestResolvePriorityExplicit(t *testing.T) {
	for i, s := range []string{"-2", "-1", "0", "1", "2"} {
		got, err := resolvePriority(s, "info")
		require.NoError(t, err)
		require.Equal(t, i-2, got, "knob=%q", s)
	}
	_, err := resolvePriority("3", "info")
	require.Error(t, err)
	_, err = resolvePriority("bad", "info")
	require.Error(t, err)
}

// ---- JSON round-trip for api response decode --------------------------------

// TestSendOKBodyNotJSON ensures that a non-JSON 200 body does not cause an
// error (we tolerate bad decode and trust HTTP 200).
func TestSendOKBodyNotJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(srv.Close)

	p := newPluginForTest(t)
	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{Meta: baseMeta(srv.URL)})
	require.NoError(t, err)
}

// ---- parseTimeout & metaString unit coverage --------------------------------

func TestParseTimeout(t *testing.T) {
	d, ok := parseTimeout("5s")
	require.True(t, ok)
	require.Equal(t, 5*time.Second, d)

	d, ok = parseTimeout(float64(3))
	require.True(t, ok)
	require.Equal(t, 3*time.Second, d)

	d, ok = parseTimeout(int(7))
	require.True(t, ok)
	require.Equal(t, 7*time.Second, d)

	_, ok = parseTimeout("nope")
	require.False(t, ok)

	_, ok = parseTimeout(nil)
	require.False(t, ok)
}

func TestMetaString(t *testing.T) {
	m := map[string]any{"k": "val", "n": float64(42)}
	v, ok := metaString(m, "k")
	require.True(t, ok)
	require.Equal(t, "val", v)

	_, ok = metaString(m, "missing")
	require.False(t, ok)

	v, ok = metaString(m, "n")
	require.True(t, ok)
	require.Equal(t, "42", v)
}

// ---- apiBase trailing-slash normalisation -----------------------------------

func TestAPIBaseTrailingSlash(t *testing.T) {
	var (
		mu      sync.Mutex
		gotPath string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotPath = r.URL.Path
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":1}`))
	}))
	t.Cleanup(srv.Close)

	p := newPluginForTest(t)
	meta := baseMeta(srv.URL + "/") // trailing slash
	require.NoError(t, p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{Meta: meta}))

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, "/1/messages.json", gotPath)
}

// ---- configFromMeta JSON number (float64) support ---------------------------

// TestConfigFromMetaJSONNumbers verifies that priority delivered as a float64
// (as produced by JSON unmarshal) is handled gracefully via the string
// conversion path.
func TestConfigFromMetaJSONNumbers(t *testing.T) {
	var (
		mu      sync.Mutex
		gotForm url.Values
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f := parseForm(t, r)
		mu.Lock()
		gotForm = f
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":1}`))
	}))
	t.Cleanup(srv.Close)

	// Simulate how the metadata pipeline might store the selector value as
	// a JSON-decoded float64.
	raw := map[string]any{
		"token":    "t",
		"user":     "u",
		"api_base": srv.URL,
		"priority": "1", // explicit high priority as string
		"timeout":  float64(5),
	}
	// Roundtrip through JSON to simulate real-world decode.
	b, _ := json.Marshal(raw)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(b, &decoded))

	p := newPluginForTest(t)
	require.NoError(t, p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{Meta: decoded}))

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, "1", gotForm.Get("priority"))
}
