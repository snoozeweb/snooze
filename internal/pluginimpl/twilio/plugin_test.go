package twilio

import (
	"context"
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

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func sampleRecord() snoozetypes.Record {
	return snoozetypes.Record{
		UID:      "rec-1",
		Host:     "db-1.example.com",
		Source:   "syslog",
		Severity: "warning",
		Message:  "disk full",
	}
}

// baseMeta returns a minimal valid action_form Meta map pointing at the given
// server URL.
func baseMeta(apiBase string) map[string]any {
	return map[string]any{
		"account_sid": "ACtest000000000000000000000000001",
		"auth_token":  "secret-token",
		"from":        "+15005550006",
		"to":          "+15005550007",
		"api_base":    apiBase,
		"timeout":     "5s",
	}
}

// newPluginForTest builds a Plugin whose HTTP client uses the plain default
// transport (no TLS override needed for httptest.NewServer).
func newPluginForTest(t *testing.T) *Plugin {
	t.Helper()
	p, err := factory(plugins.Metadata{Name: "twilio"})
	require.NoError(t, err)
	tp, ok := p.(*Plugin)
	require.True(t, ok)
	// Override the client builder so tests control the timeout but use the
	// default transport — httptest.NewServer is plain HTTP.
	tp.newClient = func(timeout time.Duration) *http.Client {
		if timeout <= 0 {
			timeout = defaultTimeout
		}
		return &http.Client{Timeout: timeout}
	}
	return tp
}

// capture holds the data a test server recorded from a single inbound request.
type capture struct {
	mu      sync.Mutex
	entries []requestEntry
}

type requestEntry struct {
	method   string
	path     string
	authUser string
	authPass string
	form     url.Values
}

func (c *capture) add(e requestEntry) {
	c.mu.Lock()
	c.entries = append(c.entries, e)
	c.mu.Unlock()
}

func (c *capture) snapshot() []requestEntry {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]requestEntry, len(c.entries))
	copy(out, c.entries)
	return out
}

// newRecordingServer returns an httptest server that records every request
// and replies with the provided status and body. Pass statusCode=201 for the
// happy path.
func newRecordingServer(t *testing.T, statusCode int, responseBody string) (*httptest.Server, *capture) {
	t.Helper()
	capt := &capture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = r.ParseForm()
		form, _ := url.ParseQuery(string(body))
		u, pw, _ := r.BasicAuth()
		capt.add(requestEntry{
			method:   r.Method,
			path:     r.URL.Path,
			authUser: u,
			authPass: pw,
			form:     form,
		})
		w.WriteHeader(statusCode)
		_, _ = io.WriteString(w, responseBody)
	}))
	t.Cleanup(srv.Close)
	return srv, capt
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestRegistration(t *testing.T) {
	require.True(t, slices.Contains(plugins.Registered(), "twilio"))
}

func TestPluginInterfaceContract(t *testing.T) {
	var _ plugins.Plugin = (*Plugin)(nil)
	var _ plugins.Notifier = (*Plugin)(nil)

	p, err := factory(plugins.Metadata{Name: "twilio"})
	require.NoError(t, err)
	require.Equal(t, "twilio", p.Name())
	require.NoError(t, p.PostInit(context.Background(), nil))
	require.NoError(t, p.Reload(context.Background()))
}

// TestSendSMS verifies that a successful SMS send:
//   - hits /2010-04-01/Accounts/<sid>/Messages.json via POST,
//   - includes the correct Basic auth credentials,
//   - sends the rendered Body, To, and From form fields.
func TestSendSMS(t *testing.T) {
	srv, capt := newRecordingServer(t, http.StatusCreated, `{"sid":"SM123","status":"queued"}`)

	p := newPluginForTest(t)
	rec := sampleRecord()
	meta := baseMeta(srv.URL)
	meta["mode"] = "sms"
	meta["message"] = "{{ .Severity }} on {{ .Host }}: {{ .Message }}"

	err := p.Send(context.Background(), rec, plugins.NotificationPayload{Meta: meta})
	require.NoError(t, err)

	entries := capt.snapshot()
	require.Len(t, entries, 1)
	e := entries[0]

	require.Equal(t, http.MethodPost, e.method)
	require.Equal(t, "/2010-04-01/Accounts/ACtest000000000000000000000000001/Messages.json", e.path)
	require.Equal(t, "ACtest000000000000000000000000001", e.authUser, "Basic auth user must be account SID")
	require.Equal(t, "secret-token", e.authPass, "Basic auth password must be auth token")
	require.Equal(t, "+15005550007", e.form.Get("To"))
	require.Equal(t, "+15005550006", e.form.Get("From"))
	require.Equal(t, "warning on db-1.example.com: disk full", e.form.Get("Body"))
}

// TestSendSMSDefaultTemplate verifies that the default message template is
// applied when the action_form omits the message field.
func TestSendSMSDefaultTemplate(t *testing.T) {
	srv, capt := newRecordingServer(t, http.StatusCreated, `{"sid":"SM999"}`)

	p := newPluginForTest(t)
	meta := baseMeta(srv.URL) // no "message" key → default template

	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{Meta: meta})
	require.NoError(t, err)
	require.Len(t, capt.snapshot(), 1)
	require.Equal(t, "warning on db-1.example.com: disk full", capt.snapshot()[0].form.Get("Body"))
}

// TestSendSMSMultipleRecipients verifies that each comma-separated recipient
// generates exactly one request and that all are attempted even when one fails.
func TestSendSMSMultipleRecipients(t *testing.T) {
	srv, capt := newRecordingServer(t, http.StatusCreated, `{"sid":"SM456"}`)

	p := newPluginForTest(t)
	meta := baseMeta(srv.URL)
	meta["to"] = "+15005550007,+15005550008,+15005550009"

	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{Meta: meta})
	require.NoError(t, err)

	entries := capt.snapshot()
	require.Len(t, entries, 3, "one request per recipient")

	var tos []string
	for _, e := range entries {
		tos = append(tos, e.form.Get("To"))
	}
	require.ElementsMatch(t, []string{"+15005550007", "+15005550008", "+15005550009"}, tos)
}

// TestSendSMSMultiRecipientsPartialFailure ensures all recipients are attempted
// and an error is returned when any one fails.
func TestSendSMSMultiRecipientsPartialFailure(t *testing.T) {
	var mu sync.Mutex
	call := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		n := call
		call++
		mu.Unlock()
		if n == 1 {
			// second recipient fails
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"message":"Invalid To","code":21211}`)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"sid":"SMok"}`)
	}))
	t.Cleanup(srv.Close)

	p := newPluginForTest(t)
	meta := baseMeta(srv.URL)
	meta["to"] = "+15005550007,+15005550008,+15005550009"

	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{Meta: meta})
	require.Error(t, err, "partial failure must propagate as an error")
	// All three requests must still have been sent.
	mu.Lock()
	require.Equal(t, 3, call, "all recipients must be attempted")
	mu.Unlock()
}

// TestSendVoice verifies that voice mode hits .../Calls.json and sends a
// Twiml field whose <Say> contains the rendered voice_message text.
func TestSendVoice(t *testing.T) {
	srv, capt := newRecordingServer(t, http.StatusCreated, `{"sid":"CA001","status":"queued"}`)

	p := newPluginForTest(t)
	meta := baseMeta(srv.URL)
	meta["mode"] = "voice"
	meta["voice_message"] = "Snooze alert. {{ .Severity }} on {{ .Host }}. {{ .Message }}"

	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{Meta: meta})
	require.NoError(t, err)

	entries := capt.snapshot()
	require.Len(t, entries, 1)
	e := entries[0]

	require.Equal(t, "/2010-04-01/Accounts/ACtest000000000000000000000000001/Calls.json", e.path)
	require.Equal(t, "+15005550007", e.form.Get("To"))
	require.Equal(t, "+15005550006", e.form.Get("From"))

	twiml := e.form.Get("Twiml")
	require.Contains(t, twiml, "<Say>", "Twiml must contain a <Say> element")
	require.Contains(t, twiml, "Snooze alert. warning on db-1.example.com. disk full")
}

// TestSendVoiceXMLEscaping verifies that characters special in XML are escaped
// in the <Say> body so they don't break the TwiML document.
func TestSendVoiceXMLEscaping(t *testing.T) {
	srv, capt := newRecordingServer(t, http.StatusCreated, `{"sid":"CA002"}`)

	p := newPluginForTest(t)
	rec := snoozetypes.Record{
		UID:      "rec-2",
		Host:     "host<1>",
		Severity: "critical",
		Message:  `alert: "high & low" <test>`,
	}
	meta := baseMeta(srv.URL)
	meta["mode"] = "voice"
	// Use the default voice_message template — it will embed the dangerous chars.
	delete(meta, "voice_message")

	err := p.Send(context.Background(), rec, plugins.NotificationPayload{Meta: meta})
	require.NoError(t, err)

	twiml := capt.snapshot()[0].form.Get("Twiml")
	require.NotContains(t, twiml, `"high & low"`, "raw & must be escaped in TwiML")
	require.Contains(t, twiml, "&amp;", "& must be escaped as &amp;")
}

// TestSendNon2xx verifies that a non-2xx response is surfaced as an error
// that includes the HTTP status code.
func TestSendNon2xx(t *testing.T) {
	srv, _ := newRecordingServer(t, http.StatusUnauthorized, `{"message":"Authenticate","code":20003}`)

	p := newPluginForTest(t)
	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{Meta: baseMeta(srv.URL)})
	require.Error(t, err)
	require.Contains(t, err.Error(), "401")
}

// TestSendMissingAccountSID verifies that an empty account_sid is rejected
// before any network call is made.
func TestSendMissingAccountSID(t *testing.T) {
	p := newPluginForTest(t)
	meta := map[string]any{
		// account_sid intentionally absent
		"auth_token": "tok",
		"from":       "+15005550006",
		"to":         "+15005550007",
	}
	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{Meta: meta})
	require.Error(t, err)
	require.Contains(t, err.Error(), "account_sid")
}

// TestSendMissingAuthToken verifies that an empty auth_token is rejected.
func TestSendMissingAuthToken(t *testing.T) {
	p := newPluginForTest(t)
	meta := map[string]any{
		"account_sid": "ACtest",
		// auth_token intentionally absent
		"from": "+15005550006",
		"to":   "+15005550007",
	}
	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{Meta: meta})
	require.Error(t, err)
	require.Contains(t, err.Error(), "auth_token")
}

// TestSendMissingFrom verifies that an empty from field is rejected.
func TestSendMissingFrom(t *testing.T) {
	p := newPluginForTest(t)
	meta := map[string]any{
		"account_sid": "ACtest",
		"auth_token":  "tok",
		// from intentionally absent
		"to": "+15005550007",
	}
	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{Meta: meta})
	require.Error(t, err)
	require.Contains(t, err.Error(), "from")
}

// TestSendMissingTo verifies that an empty to field is rejected.
func TestSendMissingTo(t *testing.T) {
	p := newPluginForTest(t)
	meta := map[string]any{
		"account_sid": "ACtest",
		"auth_token":  "tok",
		"from":        "+15005550006",
		// to intentionally absent
	}
	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{Meta: meta})
	require.Error(t, err)
	require.Contains(t, err.Error(), "to")
}

// TestSendRace verifies that concurrent calls from different goroutines do not
// trigger the race detector (Send must not share mutable state).
func TestSendRace(t *testing.T) {
	srv, _ := newRecordingServer(t, http.StatusCreated, `{"sid":"SMrace"}`)

	p := newPluginForTest(t)
	meta := baseMeta(srv.URL)

	const goroutines = 10
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			_ = p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{Meta: meta})
		}()
	}
	wg.Wait()
}
