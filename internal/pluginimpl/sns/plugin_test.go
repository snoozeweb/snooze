package sns

import (
	"context"
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

func TestRegistration(t *testing.T) {
	require.True(t, slices.Contains(plugins.Registered(), "sns"))
}

func TestPluginInterfaceContract(t *testing.T) {
	var _ plugins.Plugin = (*Plugin)(nil)
	var _ plugins.Notifier = (*Plugin)(nil)
	p, err := factory(plugins.Metadata{Name: "sns"})
	require.NoError(t, err)
	require.Equal(t, "sns", p.Name())
	require.NoError(t, p.PostInit(context.Background(), nil))
	require.NoError(t, p.Reload(context.Background()))
}

func newPluginForTest(t *testing.T) *Plugin {
	t.Helper()
	p, err := factory(plugins.Metadata{Name: "sns"})
	require.NoError(t, err)
	sp, ok := p.(*Plugin)
	require.True(t, ok)
	sp.newClient = func(timeout time.Duration) *http.Client {
		if timeout <= 0 {
			timeout = defaultTimeout
		}
		return &http.Client{Timeout: timeout}
	}
	return sp
}

func sampleRecord() snoozetypes.Record {
	return snoozetypes.Record{
		UID:      "rec-1",
		Host:     "db-1.example.com",
		Source:   "syslog",
		Severity: "warning",
		Message:  "disk full",
	}
}

const okPublishXML = `<?xml version="1.0"?>
<PublishResponse xmlns="http://sns.amazonaws.com/doc/2010-03-31/">
  <PublishResult><MessageId>567910cd-659e-55d4-8ccb-5aaf14679dc0</MessageId></PublishResult>
  <ResponseMetadata><RequestId>d74b8436-ae13-5ab4-a9ff-ce54dfea72a0</RequestId></ResponseMetadata>
</PublishResponse>`

// capture holds the inspected request fields behind a mutex so `go test -race`
// stays clean even though the httptest handler runs on its own goroutine.
type capture struct {
	mu        sync.Mutex
	auth      string
	amzDate   string
	secTok    string
	ctype     string
	form      url.Values
	gotMethod string
}

func (c *capture) record(r *http.Request) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gotMethod = r.Method
	c.auth = r.Header.Get("Authorization")
	c.amzDate = r.Header.Get("X-Amz-Date")
	c.secTok = r.Header.Get("X-Amz-Security-Token")
	c.ctype = r.Header.Get("Content-Type")
	_ = r.ParseForm()
	c.form = r.PostForm
}

func (c *capture) snapshot() capture {
	c.mu.Lock()
	defer c.mu.Unlock()
	return capture{
		auth: c.auth, amzDate: c.amzDate, secTok: c.secTok,
		ctype: c.ctype, form: c.form, gotMethod: c.gotMethod,
	}
}

func baseMeta(endpoint string) map[string]any {
	return map[string]any{
		"region":            "eu-west-1",
		"topic_arn":         "arn:aws:sns:eu-west-1:123456789012:alerts",
		"access_key_id":     "AKIDEXAMPLE",
		"secret_access_key": "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY",
		"endpoint":          endpoint,
	}
}

func TestPublish(t *testing.T) {
	capt := &capture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capt.record(r)
		w.Header().Set("Content-Type", "text/xml")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(okPublishXML))
	}))
	t.Cleanup(srv.Close)

	p := newPluginForTest(t)
	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta: baseMeta(srv.URL),
	})
	require.NoError(t, err)

	got := capt.snapshot()
	require.Equal(t, http.MethodPost, got.gotMethod)
	require.Equal(t, "application/x-www-form-urlencoded", got.ctype)
	require.True(t, len(got.auth) > 0)
	require.Contains(t, got.auth, "AWS4-HMAC-SHA256 Credential=")
	require.Contains(t, got.auth, "SignedHeaders=")
	require.Contains(t, got.auth, "Signature=")
	require.NotEmpty(t, got.amzDate, "X-Amz-Date must be present")
	require.Empty(t, got.secTok, "no session token configured")

	require.Equal(t, "Publish", got.form.Get("Action"))
	require.Equal(t, "2010-03-31", got.form.Get("Version"))
	require.Equal(t, "arn:aws:sns:eu-west-1:123456789012:alerts", got.form.Get("TopicArn"))
	require.Equal(t, "disk full", got.form.Get("Message"))
	require.Equal(t, "warning on db-1.example.com", got.form.Get("Subject"))
}

func TestPublishCustomTemplates(t *testing.T) {
	capt := &capture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capt.record(r)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(okPublishXML))
	}))
	t.Cleanup(srv.Close)

	meta := baseMeta(srv.URL)
	meta["subject"] = "[{{ .Source }}] {{ .Severity }}"
	meta["message"] = "{{ .Host }}: {{ .Message }}"

	p := newPluginForTest(t)
	require.NoError(t, p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{Meta: meta}))

	got := capt.snapshot()
	require.Equal(t, "[syslog] warning", got.form.Get("Subject"))
	require.Equal(t, "db-1.example.com: disk full", got.form.Get("Message"))
}

func TestPublishSessionTokenAddsHeader(t *testing.T) {
	capt := &capture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capt.record(r)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(okPublishXML))
	}))
	t.Cleanup(srv.Close)

	meta := baseMeta(srv.URL)
	meta["session_token"] = "FwoGZXIvYXdzEND..."

	p := newPluginForTest(t)
	require.NoError(t, p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{Meta: meta}))

	got := capt.snapshot()
	require.Equal(t, "FwoGZXIvYXdzEND...", got.secTok, "X-Amz-Security-Token must be set")
	// The token must also be folded into the signed headers.
	require.Contains(t, got.auth, "x-amz-security-token")
}

func TestPublishErrorXML(t *testing.T) {
	const errXML = `<?xml version="1.0"?>
<ErrorResponse xmlns="http://sns.amazonaws.com/doc/2010-03-31/">
  <Error>
    <Type>Sender</Type>
    <Code>AuthorizationError</Code>
    <Message>User is not authorized to perform: SNS:Publish on resource</Message>
  </Error>
  <RequestId>abc</RequestId>
</ErrorResponse>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(errXML))
	}))
	t.Cleanup(srv.Close)

	p := newPluginForTest(t)
	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{Meta: baseMeta(srv.URL)})
	require.Error(t, err)
	require.Contains(t, err.Error(), "403")
	require.Contains(t, err.Error(), "AuthorizationError")
	require.Contains(t, err.Error(), "not authorized")
}

func TestPublishNon2xxNonXML(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("upstream boom"))
	}))
	t.Cleanup(srv.Close)

	p := newPluginForTest(t)
	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{Meta: baseMeta(srv.URL)})
	require.Error(t, err)
	require.Contains(t, err.Error(), "502")
	require.Contains(t, err.Error(), "upstream boom")
}

func TestSendMissingConfig(t *testing.T) {
	p := newPluginForTest(t)

	// Wholly empty meta.
	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{Meta: map[string]any{}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "region")

	// Missing only the secret.
	err = p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta: map[string]any{
			"region":        "eu-west-1",
			"topic_arn":     "arn:aws:sns:eu-west-1:1:alerts",
			"access_key_id": "AKID",
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "secret_access_key")
}

func TestConfigDefaultsAndTimeout(t *testing.T) {
	cfg, err := configFromMeta(map[string]any{
		"region":            "us-east-1",
		"topic_arn":         "arn:aws:sns:us-east-1:1:t",
		"access_key_id":     "AKID",
		"secret_access_key": "secret",
		"timeout":           "3s",
	})
	require.NoError(t, err)
	require.Equal(t, defaultSubjectTmpl, cfg.Subject)
	require.Equal(t, defaultMessageTmpl, cfg.Message)
	require.Equal(t, 3*time.Second, cfg.Timeout)
	require.Empty(t, cfg.Endpoint)
}

func TestParseSNSError(t *testing.T) {
	code, msg := parseSNSError([]byte(`<ErrorResponse><Error><Code>InvalidParameter</Code><Message>bad arn</Message></Error></ErrorResponse>`))
	require.Equal(t, "InvalidParameter", code)
	require.Equal(t, "bad arn", msg)

	code, msg = parseSNSError([]byte(`<PublishResponse><PublishResult><MessageId>x</MessageId></PublishResult></PublishResponse>`))
	require.Empty(t, code)
	require.Empty(t, msg)

	code, _ = parseSNSError([]byte("not xml at all"))
	require.Empty(t, code)
}
