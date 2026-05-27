package cloudwatch

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1" //nolint:gosec // SHA1 is required by the AWS SNS SignatureVersion 1 spec.
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	"github.com/snoozeweb/snooze/internal/config"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/internal/telemetry"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// fakeHost is a minimal plugins.Host that additionally satisfies the local
// recordProcessor interface used by the cloudwatch plugin. ProcessRecord
// captures every record passed in.
type fakeHost struct {
	mu      sync.Mutex
	records []snoozetypes.Record
	err     error

	// cfg, when non-nil, is returned from Config(). It lets a test enable SNS
	// signature verification via Ingest.SNSVerify. When nil the host returns
	// config.Default() (verify off), matching production defaults.
	cfg *config.Config
}

func (h *fakeHost) DB() db.Driver                { return nil }
func (h *fakeHost) Bus() plugins.Bus             { return nil }
func (h *fakeHost) Logger() *slog.Logger         { return slog.Default() }
func (h *fakeHost) Tracer() trace.Tracer         { return otel.Tracer("cloudwatch-test") }
func (h *fakeHost) Metrics() *telemetry.Registry { return telemetry.NewRegistry(nil) }
func (h *fakeHost) Config() *config.Config {
	if h.cfg != nil {
		return h.cfg
	}
	return config.Default()
}
func (h *fakeHost) Plugin(string) plugins.Plugin { return nil }

// hostWithSNSVerify builds a fakeHost whose Config().Ingest.SNSVerify is true,
// enabling SNS message-signature verification.
func hostWithSNSVerify() *fakeHost {
	cfg := config.Default()
	cfg.Ingest.SNSVerify = true
	return &fakeHost{cfg: cfg}
}

// ProcessRecord makes *fakeHost satisfy the plugin's internal recordProcessor
// runtime assertion.
func (h *fakeHost) ProcessRecord(_ context.Context, rec snoozetypes.Record) (snoozetypes.Record, plugins.Action, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, rec)
	if h.err != nil {
		return rec, plugins.ActionAbort, h.err
	}
	return rec, plugins.ActionContinue, nil
}

func (h *fakeHost) seen() []snoozetypes.Record {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]snoozetypes.Record, len(h.records))
	copy(out, h.records)
	return out
}

func newPlugin(t *testing.T, host plugins.Host) *Plugin {
	t.Helper()
	p := &Plugin{meta: plugins.Metadata{Name: "cloudwatch"}}
	require.NoError(t, p.PostInit(context.Background(), host))
	return p
}

// postWebhook posts body with optional extra headers to the plugin's
// HandleWebhook and returns the recorded response.
func postWebhook(t *testing.T, p *Plugin, body []byte, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhook/cloudwatch", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	p.HandleWebhook(w, req)
	return w
}

// -- Registration & contract -------------------------------------------------

func TestRegistration(t *testing.T) {
	require.True(t, slices.Contains(plugins.Registered(), "cloudwatch"))
}

func TestPluginContract(t *testing.T) {
	p := &Plugin{meta: plugins.Metadata{Name: "cloudwatch"}}
	require.Equal(t, "cloudwatch", p.Name())
	require.Equal(t, "/cloudwatch", p.WebhookPath())
	require.Equal(t, "cloudwatch", p.Metadata().Name)
	require.NoError(t, p.Reload(context.Background()))

	// Ensure the plugin satisfies the WebhookReceiver interface at compile time.
	var _ plugins.WebhookReceiver = p
}

// -- SubscriptionConfirmation ------------------------------------------------

func TestSubscriptionConfirmationGETsSubscribeURLAndEmitsNoRecords(t *testing.T) {
	// Start a confirmation server that records whether it was called.
	var hit atomic.Bool
	confirmSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hit.Store(true)
		w.WriteHeader(http.StatusOK)
	}))
	defer confirmSrv.Close()

	host := &fakeHost{}
	p := newPlugin(t, host)
	// Override the confirmClient to point at our local httptest server.
	p.confirmClient = func() *http.Client { return confirmSrv.Client() }

	body, _ := json.Marshal(map[string]string{
		"Type":         "SubscriptionConfirmation",
		"TopicArn":     "arn:aws:sns:us-east-1:123456789012:MyTopic",
		"SubscribeURL": confirmSrv.URL + "/confirm",
		"Token":        "abc123",
	})
	w := postWebhook(t, p, body, map[string]string{
		"x-amz-sns-message-type": "SubscriptionConfirmation",
	})
	require.Equal(t, http.StatusOK, w.Code)

	// The confirmation server must have been called.
	require.True(t, hit.Load(), "confirmSrv must have been hit")

	// No records should have been emitted.
	require.Empty(t, host.seen())
}

func TestSubscriptionConfirmationFallsBackToTypeField(t *testing.T) {
	// Same test but without the header — relies on the JSON Type field.
	var hit atomic.Bool
	confirmSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hit.Store(true)
		w.WriteHeader(http.StatusOK)
	}))
	defer confirmSrv.Close()

	host := &fakeHost{}
	p := newPlugin(t, host)
	p.confirmClient = func() *http.Client { return confirmSrv.Client() }

	body, _ := json.Marshal(map[string]string{
		"Type":         "SubscriptionConfirmation",
		"SubscribeURL": confirmSrv.URL + "/confirm",
	})
	// No x-amz-sns-message-type header — plugin falls back to JSON Type.
	w := postWebhook(t, p, body, nil)
	require.Equal(t, http.StatusOK, w.Code)
	require.True(t, hit.Load(), "confirmSrv must have been hit")
	require.Empty(t, host.seen())
}

// -- Notification / ALARM state ----------------------------------------------

func TestNotificationAlarmEmitsOneRecord(t *testing.T) {
	host := &fakeHost{}
	p := newPlugin(t, host)

	// The inner CloudWatch alarm JSON is itself a JSON string inside Message.
	alarmJSON, _ := json.Marshal(map[string]any{
		"AlarmName":        "HighCPU",
		"AlarmDescription": "CPU too high",
		"NewStateValue":    "ALARM",
		"NewStateReason":   "Threshold crossed",
		"Region":           "us-east-1",
		"StateChangeTime":  "2024-01-15T12:00:00.000+0000",
		"Trigger": map[string]any{
			"Namespace":  "AWS/EC2",
			"MetricName": "CPUUtilization",
			"Dimensions": []map[string]string{
				{"name": "InstanceId", "value": "i-0abc123def456"},
			},
		},
	})

	envelope, _ := json.Marshal(map[string]string{
		"Type":      "Notification",
		"MessageId": "msg-001",
		"TopicArn":  "arn:aws:sns:us-east-1:123456789012:AlarmsTopic",
		"Subject":   "ALARM: HighCPU",
		"Message":   string(alarmJSON),
		"Timestamp": "2024-01-15T12:00:01.000Z",
	})

	w := postWebhook(t, p, envelope, map[string]string{
		"x-amz-sns-message-type": "Notification",
	})
	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, "ok", resp["status"])
	require.EqualValues(t, 1, resp["received"])
	require.EqualValues(t, 1, resp["accepted"])

	recs := host.seen()
	require.Len(t, recs, 1)
	rec := recs[0]

	require.Equal(t, "cloudwatch", rec.Source)
	require.Equal(t, "i-0abc123def456", rec.Host)
	require.Equal(t, "CPUUtilization", rec.Process)
	require.Equal(t, "critical", rec.Severity)
	require.Empty(t, rec.State) // ALARM → no close state
	require.Equal(t, "Threshold crossed", rec.Message)
	require.Equal(t, "HighCPU", rec.Raw["alarmName"])
	require.Equal(t, "ALARM", rec.Raw["newStateValue"])
	require.Equal(t, "arn:aws:sns:us-east-1:123456789012:AlarmsTopic", rec.Raw["topicArn"])
}

// -- Notification / OK state (resolve) ---------------------------------------

func TestNotificationOKEmitsCloseRecord(t *testing.T) {
	host := &fakeHost{}
	p := newPlugin(t, host)

	alarmJSON, _ := json.Marshal(map[string]any{
		"AlarmName":       "HighCPU",
		"NewStateValue":   "OK",
		"NewStateReason":  "Threshold no longer breached",
		"Region":          "us-east-1",
		"StateChangeTime": "2024-01-15T13:00:00.000+0000",
		"Trigger": map[string]any{
			"Namespace":  "AWS/EC2",
			"MetricName": "CPUUtilization",
			"Dimensions": []map[string]string{
				{"name": "InstanceId", "value": "i-0abc123def456"},
			},
		},
	})

	envelope, _ := json.Marshal(map[string]string{
		"Type":     "Notification",
		"TopicArn": "arn:aws:sns:us-east-1:123456789012:AlarmsTopic",
		"Message":  string(alarmJSON),
	})

	w := postWebhook(t, p, envelope, map[string]string{
		"x-amz-sns-message-type": "Notification",
	})
	require.Equal(t, http.StatusOK, w.Code)

	recs := host.seen()
	require.Len(t, recs, 1)
	rec := recs[0]

	require.Equal(t, "info", rec.Severity)
	require.Equal(t, "close", rec.State)
	require.Equal(t, "cloudwatch", rec.Source)
	require.Equal(t, "i-0abc123def456", rec.Host)
}

// -- Notification / INSUFFICIENT_DATA state ----------------------------------

func TestNotificationInsufficientDataEmitsWarning(t *testing.T) {
	host := &fakeHost{}
	p := newPlugin(t, host)

	alarmJSON, _ := json.Marshal(map[string]any{
		"AlarmName":      "DiskCheck",
		"NewStateValue":  "INSUFFICIENT_DATA",
		"NewStateReason": "Insufficient data for evaluation",
		"Trigger": map[string]any{
			"Namespace":  "AWS/EBS",
			"MetricName": "VolumeReadBytes",
			"Dimensions": []map[string]string{},
		},
	})

	envelope, _ := json.Marshal(map[string]string{
		"Type":     "Notification",
		"TopicArn": "arn:aws:sns:us-east-1:123456789012:AlarmsTopic",
		"Message":  string(alarmJSON),
	})

	w := postWebhook(t, p, envelope, map[string]string{
		"x-amz-sns-message-type": "Notification",
	})
	require.Equal(t, http.StatusOK, w.Code)

	recs := host.seen()
	require.Len(t, recs, 1)
	require.Equal(t, "warning", recs[0].Severity)
	require.Empty(t, recs[0].State)
	require.Equal(t, "DiskCheck", recs[0].Host) // no InstanceId dim → fallback to AlarmName
}

// -- Host fallback when no InstanceId dimension ------------------------------

func TestNotificationHostFallsBackToAlarmName(t *testing.T) {
	host := &fakeHost{}
	p := newPlugin(t, host)

	alarmJSON, _ := json.Marshal(map[string]any{
		"AlarmName":      "BillingAlarm",
		"NewStateValue":  "ALARM",
		"NewStateReason": "Billing threshold exceeded",
		"Trigger": map[string]any{
			"Namespace":  "AWS/Billing",
			"MetricName": "EstimatedCharges",
			"Dimensions": []map[string]string{
				{"name": "Currency", "value": "USD"},
			},
		},
	})

	envelope, _ := json.Marshal(map[string]string{
		"Type":     "Notification",
		"TopicArn": "arn:aws:sns:us-east-1:123456789012:Billing",
		"Message":  string(alarmJSON),
	})

	w := postWebhook(t, p, envelope, map[string]string{
		"x-amz-sns-message-type": "Notification",
	})
	require.Equal(t, http.StatusOK, w.Code)

	recs := host.seen()
	require.Len(t, recs, 1)
	require.Equal(t, "BillingAlarm", recs[0].Host)
	require.Equal(t, "EstimatedCharges", recs[0].Process)
}

// -- Message fallback to AlarmDescription ------------------------------------

func TestNotificationMessageFallsBackToAlarmDescription(t *testing.T) {
	host := &fakeHost{}
	p := newPlugin(t, host)

	alarmJSON, _ := json.Marshal(map[string]any{
		"AlarmName":        "CPUAlarm",
		"AlarmDescription": "CPU usage too high on prod",
		"NewStateValue":    "ALARM",
		"NewStateReason":   "", // empty — should fall back to description
		"Trigger": map[string]any{
			"Namespace":  "AWS/EC2",
			"MetricName": "CPUUtilization",
			"Dimensions": []map[string]string{},
		},
	})

	envelope, _ := json.Marshal(map[string]string{
		"Type":    "Notification",
		"Message": string(alarmJSON),
	})

	w := postWebhook(t, p, envelope, nil)
	require.Equal(t, http.StatusOK, w.Code)

	recs := host.seen()
	require.Len(t, recs, 1)
	require.Equal(t, "CPU usage too high on prod", recs[0].Message)
}

// -- Malformed outer JSON → 400 ----------------------------------------------

func TestMalformedOuterJSONReturns400(t *testing.T) {
	host := &fakeHost{}
	p := newPlugin(t, host)

	w := postWebhook(t, p, []byte(`{not-json`), map[string]string{
		"x-amz-sns-message-type": "Notification",
	})
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Empty(t, host.seen())
	require.True(t,
		strings.Contains(w.Body.String(), "invalid SNS payload"),
		"body=%q", w.Body.String(),
	)
}

// -- Wrong method → 405 ------------------------------------------------------

func TestWrongMethodReturns405(t *testing.T) {
	p := newPlugin(t, &fakeHost{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/webhook/cloudwatch", nil)
	w := httptest.NewRecorder()
	p.HandleWebhook(w, req)
	require.Equal(t, http.StatusMethodNotAllowed, w.Code)
	require.Equal(t, http.MethodPost, w.Header().Get("Allow"))
}

// -- Pipeline error is not fatal ---------------------------------------------

func TestPipelineErrorIsNotFatal(t *testing.T) {
	host := &fakeHost{err: errors.New("pipeline boom")}
	p := newPlugin(t, host)

	alarmJSON, _ := json.Marshal(map[string]any{
		"AlarmName":      "Test",
		"NewStateValue":  "ALARM",
		"NewStateReason": "test",
		"Trigger": map[string]any{
			"Namespace":  "AWS/EC2",
			"MetricName": "CPUUtilization",
			"Dimensions": []map[string]string{},
		},
	})
	envelope, _ := json.Marshal(map[string]string{
		"Type":    "Notification",
		"Message": string(alarmJSON),
	})

	w := postWebhook(t, p, envelope, map[string]string{
		"x-amz-sns-message-type": "Notification",
	})
	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.EqualValues(t, 1, resp["received"])
	// Pipeline error → not accepted, but response is still 200.
	require.EqualValues(t, 0, resp["accepted"])
	// The record was still passed to ProcessRecord.
	require.Len(t, host.seen(), 1)
}

// -- No processor degrades gracefully ----------------------------------------

func TestNoRecordProcessorDegradesGracefully(t *testing.T) {
	p := &Plugin{meta: plugins.Metadata{Name: "cloudwatch"}}
	// PostInit with a host that does NOT satisfy recordProcessor.
	require.NoError(t, p.PostInit(context.Background(), &nakedPluginHost{}))

	alarmJSON, _ := json.Marshal(map[string]any{
		"AlarmName":      "Test",
		"NewStateValue":  "ALARM",
		"NewStateReason": "test",
		"Trigger": map[string]any{
			"Namespace":  "AWS/EC2",
			"MetricName": "CPUUtilization",
			"Dimensions": []map[string]string{},
		},
	})
	envelope, _ := json.Marshal(map[string]string{
		"Type":    "Notification",
		"Message": string(alarmJSON),
	})

	w := postWebhook(t, p, envelope, map[string]string{
		"x-amz-sns-message-type": "Notification",
	})
	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.EqualValues(t, 1, resp["received"])
	// With no pipeline, record still counts as accepted (no-op success).
	require.EqualValues(t, 1, resp["accepted"])
}

// -- UnsubscribeConfirmation -------------------------------------------------

func TestUnsubscribeConfirmationResponds200NoRecords(t *testing.T) {
	host := &fakeHost{}
	p := newPlugin(t, host)

	body, _ := json.Marshal(map[string]string{
		"Type":     "UnsubscribeConfirmation",
		"TopicArn": "arn:aws:sns:us-east-1:123456789012:MyTopic",
	})
	w := postWebhook(t, p, body, map[string]string{
		"x-amz-sns-message-type": "UnsubscribeConfirmation",
	})
	require.Equal(t, http.StatusOK, w.Code)
	require.Empty(t, host.seen())
}

// -- Non-alarm Message field (malformed inner JSON) --------------------------

func TestNotificationWithNonAlarmMessageBuildsMinimalRecord(t *testing.T) {
	host := &fakeHost{}
	p := newPlugin(t, host)

	envelope, _ := json.Marshal(map[string]string{
		"Type":     "Notification",
		"TopicArn": "arn:aws:sns:us-east-1:123456789012:OtherTopic",
		"Subject":  "Test notification",
		"Message":  "This is a plain text message, not JSON",
	})

	w := postWebhook(t, p, envelope, map[string]string{
		"x-amz-sns-message-type": "Notification",
	})
	require.Equal(t, http.StatusOK, w.Code)

	recs := host.seen()
	require.Len(t, recs, 1)
	require.Equal(t, "cloudwatch", recs[0].Source)
	require.Equal(t, "This is a plain text message, not JSON", recs[0].Message)
}

// ---------------------------------------------------------------------------
// SNS signature verification (opt-in via Ingest.SNSVerify)
// ---------------------------------------------------------------------------

// testKeypair holds a self-signed RSA cert (PEM) and the private key used to
// sign canonical strings in tests.
type testKeypair struct {
	key     *rsa.PrivateKey
	certPEM []byte
}

// newTestKeypair generates a self-signed RSA cert+key in-test.
func newTestKeypair(t *testing.T) *testKeypair {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "sns.us-east-1.amazonaws.com"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	return &testKeypair{key: key, certPEM: certPEM}
}

// signNotification builds the canonical string-to-sign for an SNS Notification
// and returns the base64-encoded RSA signature over it. version selects the
// hash: "1" → SHA1, "2" → SHA256.
func signNotification(t *testing.T, kp *testKeypair, env map[string]string, version string) string {
	t.Helper()
	canonical := notificationCanonical(env)
	var sum []byte
	var hashAlg crypto.Hash
	switch version {
	case "2":
		h := sha256.Sum256([]byte(canonical))
		sum = h[:]
		hashAlg = crypto.SHA256
	default:
		h := sha1.Sum([]byte(canonical)) //nolint:gosec // SNS v1 spec mandates SHA1.
		sum = h[:]
		hashAlg = crypto.SHA1
	}
	sig, err := rsa.SignPKCS1v15(rand.Reader, kp.key, hashAlg, sum)
	require.NoError(t, err)
	return base64.StdEncoding.EncodeToString(sig)
}

// notificationCanonical reproduces the AWS SNS canonical string-to-sign for a
// Notification: Message, MessageId, Subject (only if present), Timestamp,
// TopicArn, Type — each as "key\nvalue\n".
func notificationCanonical(env map[string]string) string {
	var b strings.Builder
	appendKV := func(k string) {
		b.WriteString(k)
		b.WriteByte('\n')
		b.WriteString(env[k])
		b.WriteByte('\n')
	}
	appendKV("Message")
	appendKV("MessageId")
	if env["Subject"] != "" {
		appendKV("Subject")
	}
	appendKV("Timestamp")
	appendKV("TopicArn")
	appendKV("Type")
	return b.String()
}

// sampleNotificationEnv returns a Notification envelope (as a flat map) with a
// CloudWatch alarm JSON in Message.
func sampleNotificationEnv(t *testing.T) map[string]string {
	t.Helper()
	alarmJSON, err := json.Marshal(map[string]any{
		"AlarmName":      "HighCPU",
		"NewStateValue":  "ALARM",
		"NewStateReason": "Threshold crossed",
		"Trigger": map[string]any{
			"Namespace":  "AWS/EC2",
			"MetricName": "CPUUtilization",
			"Dimensions": []map[string]string{
				{"name": "InstanceId", "value": "i-0abc123def456"},
			},
		},
	})
	require.NoError(t, err)

	return map[string]string{
		"Type":      "Notification",
		"MessageId": "msg-verify-001",
		"TopicArn":  "arn:aws:sns:us-east-1:123456789012:AlarmsTopic",
		"Subject":   "ALARM: HighCPU",
		"Message":   string(alarmJSON),
		"Timestamp": "2024-01-15T12:00:01.000Z",
	}
}

// envToSigned augments env with SignatureVersion/SigningCertURL/Signature and
// returns the marshaled JSON body.
func envToSigned(t *testing.T, env map[string]string, version, certURL, signature string) []byte {
	t.Helper()
	full := map[string]string{}
	for k, v := range env {
		full[k] = v
	}
	full["SignatureVersion"] = version
	full["SigningCertURL"] = certURL
	full["Signature"] = signature
	body, err := json.Marshal(full)
	require.NoError(t, err)
	return body
}

// TestSNSVerifyAcceptsValidNotificationV1 generates a self-signed cert, signs a
// Notification's canonical string with SHA1 (SignatureVersion 1), points the
// certFetcher at the in-memory PEM, and asserts the record is processed.
func TestSNSVerifyAcceptsValidNotificationV1(t *testing.T) {
	kp := newTestKeypair(t)
	host := hostWithSNSVerify()
	p := newPlugin(t, host)

	const certURL = "https://sns.us-east-1.amazonaws.com/SimpleNotificationService-abc123.pem"
	p.certFetcher = func(_ context.Context, url string) ([]byte, error) {
		require.Equal(t, certURL, url)
		return kp.certPEM, nil
	}

	env := sampleNotificationEnv(t)
	sig := signNotification(t, kp, env, "1")
	body := envToSigned(t, env, "1", certURL, sig)

	w := postWebhook(t, p, body, map[string]string{"x-amz-sns-message-type": "Notification"})
	require.Equal(t, http.StatusOK, w.Code)

	recs := host.seen()
	require.Len(t, recs, 1)
	require.Equal(t, "i-0abc123def456", recs[0].Host)
}

// TestSNSVerifyAcceptsValidNotificationV2 is the SHA256 (SignatureVersion 2)
// variant.
func TestSNSVerifyAcceptsValidNotificationV2(t *testing.T) {
	kp := newTestKeypair(t)
	host := hostWithSNSVerify()
	p := newPlugin(t, host)

	const certURL = "https://sns.eu-west-1.amazonaws.com/SimpleNotificationService-def456.pem"
	p.certFetcher = func(_ context.Context, _ string) ([]byte, error) {
		return kp.certPEM, nil
	}

	env := sampleNotificationEnv(t)
	sig := signNotification(t, kp, env, "2")
	body := envToSigned(t, env, "2", certURL, sig)

	w := postWebhook(t, p, body, map[string]string{"x-amz-sns-message-type": "Notification"})
	require.Equal(t, http.StatusOK, w.Code)
	require.Len(t, host.seen(), 1)
}

// TestSNSVerifyRejectsTamperedSignature asserts that a bad signature → 403 and
// no record is processed.
func TestSNSVerifyRejectsTamperedSignature(t *testing.T) {
	kp := newTestKeypair(t)
	host := hostWithSNSVerify()
	p := newPlugin(t, host)

	const certURL = "https://sns.us-east-1.amazonaws.com/SimpleNotificationService-abc123.pem"
	p.certFetcher = func(_ context.Context, _ string) ([]byte, error) {
		return kp.certPEM, nil
	}

	env := sampleNotificationEnv(t)
	sig := signNotification(t, kp, env, "1")
	// Tamper: flip the signature by re-base64-ing different bytes.
	tampered := base64.StdEncoding.EncodeToString([]byte("not-a-real-signature-bytes"))
	require.NotEqual(t, sig, tampered)
	body := envToSigned(t, env, "1", certURL, tampered)

	w := postWebhook(t, p, body, map[string]string{"x-amz-sns-message-type": "Notification"})
	require.Equal(t, http.StatusForbidden, w.Code)
	require.Empty(t, host.seen())
}

// TestSNSVerifyRejectsNonAmazonCertURL asserts a SigningCertURL with a
// non-amazonaws host → 403 and the certFetcher is NOT invoked.
func TestSNSVerifyRejectsNonAmazonCertURL(t *testing.T) {
	kp := newTestKeypair(t)
	host := hostWithSNSVerify()
	p := newPlugin(t, host)

	var fetched atomic.Bool
	p.certFetcher = func(_ context.Context, _ string) ([]byte, error) {
		fetched.Store(true)
		return kp.certPEM, nil
	}

	const evilURL = "https://evil.example.com/SimpleNotificationService-abc123.pem"
	env := sampleNotificationEnv(t)
	sig := signNotification(t, kp, env, "1")
	body := envToSigned(t, env, "1", evilURL, sig)

	w := postWebhook(t, p, body, map[string]string{"x-amz-sns-message-type": "Notification"})
	require.Equal(t, http.StatusForbidden, w.Code)
	require.Empty(t, host.seen())
	require.False(t, fetched.Load(), "certFetcher must NOT be called for a non-AWS host")
}

// TestSNSVerifyConfirmsSubscriptionOnlyAfterVerification asserts the
// SubscriptionConfirmation signature is verified BEFORE the SubscribeURL is
// GET-confirmed: a bad signature → 403 and the confirm GET is NOT made.
func TestSNSVerifyConfirmsSubscriptionOnlyAfterVerification(t *testing.T) {
	kp := newTestKeypair(t)
	host := hostWithSNSVerify()
	p := newPlugin(t, host)

	var confirmed atomic.Bool
	confirmSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		confirmed.Store(true)
		w.WriteHeader(http.StatusOK)
	}))
	defer confirmSrv.Close()
	p.confirmClient = func() *http.Client { return confirmSrv.Client() }

	const certURL = "https://sns.us-east-1.amazonaws.com/SimpleNotificationService-abc123.pem"
	p.certFetcher = func(_ context.Context, _ string) ([]byte, error) {
		return kp.certPEM, nil
	}

	env := map[string]string{
		"Type":         "SubscriptionConfirmation",
		"MessageId":    "sub-001",
		"Token":        "tok-123",
		"TopicArn":     "arn:aws:sns:us-east-1:123456789012:MyTopic",
		"SubscribeURL": confirmSrv.URL + "/confirm",
		"Timestamp":    "2024-01-15T12:00:01.000Z",
		"Message":      "You have chosen to subscribe to the topic.",
	}
	// Sign with a tampered signature → must be rejected before confirm GET.
	tampered := base64.StdEncoding.EncodeToString([]byte("bad"))
	body := envToSigned(t, env, "1", certURL, tampered)

	w := postWebhook(t, p, body, map[string]string{"x-amz-sns-message-type": "SubscriptionConfirmation"})
	require.Equal(t, http.StatusForbidden, w.Code)
	require.False(t, confirmed.Load(), "SubscribeURL must NOT be confirmed when the signature is invalid")
}

// TestSNSVerifyAcceptsValidSubscriptionConfirmation builds the correct
// SubscriptionConfirmation canonical string, signs it, and asserts the
// SubscribeURL is then confirmed.
func TestSNSVerifyAcceptsValidSubscriptionConfirmation(t *testing.T) {
	kp := newTestKeypair(t)
	host := hostWithSNSVerify()
	p := newPlugin(t, host)

	var confirmed atomic.Bool
	confirmSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		confirmed.Store(true)
		w.WriteHeader(http.StatusOK)
	}))
	defer confirmSrv.Close()
	p.confirmClient = func() *http.Client { return confirmSrv.Client() }

	const certURL = "https://sns.us-east-1.amazonaws.com/SimpleNotificationService-abc123.pem"
	p.certFetcher = func(_ context.Context, _ string) ([]byte, error) {
		return kp.certPEM, nil
	}

	env := map[string]string{
		"Type":         "SubscriptionConfirmation",
		"MessageId":    "sub-001",
		"Token":        "tok-123",
		"TopicArn":     "arn:aws:sns:us-east-1:123456789012:MyTopic",
		"SubscribeURL": confirmSrv.URL + "/confirm",
		"Timestamp":    "2024-01-15T12:00:01.000Z",
		"Message":      "You have chosen to subscribe to the topic.",
	}

	// Build the SubscriptionConfirmation canonical string: Message, MessageId,
	// SubscribeURL, Timestamp, Token, TopicArn, Type.
	var sb strings.Builder
	for _, k := range []string{"Message", "MessageId", "SubscribeURL", "Timestamp", "Token", "TopicArn", "Type"} {
		sb.WriteString(k)
		sb.WriteByte('\n')
		sb.WriteString(env[k])
		sb.WriteByte('\n')
	}
	h := sha1.Sum([]byte(sb.String())) //nolint:gosec // SNS v1 spec mandates SHA1.
	sigBytes, err := rsa.SignPKCS1v15(rand.Reader, kp.key, crypto.SHA1, h[:])
	require.NoError(t, err)
	sig := base64.StdEncoding.EncodeToString(sigBytes)

	body := envToSigned(t, env, "1", certURL, sig)

	w := postWebhook(t, p, body, map[string]string{"x-amz-sns-message-type": "SubscriptionConfirmation"})
	require.Equal(t, http.StatusOK, w.Code)
	require.True(t, confirmed.Load(), "SubscribeURL must be confirmed after a valid signature")
}

// nakedPluginHost is a plugins.Host that does not satisfy recordProcessor.
type nakedPluginHost struct{}

func (nakedPluginHost) DB() db.Driver                { return nil }
func (nakedPluginHost) Bus() plugins.Bus             { return nil }
func (nakedPluginHost) Logger() *slog.Logger         { return slog.Default() }
func (nakedPluginHost) Tracer() trace.Tracer         { return otel.Tracer("cloudwatch-test") }
func (nakedPluginHost) Metrics() *telemetry.Registry { return telemetry.NewRegistry(nil) }
func (nakedPluginHost) Config() *config.Config       { return config.Default() }
func (nakedPluginHost) Plugin(string) plugins.Plugin { return nil }
