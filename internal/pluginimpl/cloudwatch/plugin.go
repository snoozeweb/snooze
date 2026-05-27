// Package cloudwatch implements the `cloudwatch` WebhookReceiver plugin.
//
// It exposes an inbound HTTP endpoint mounted under /api/v1/webhook/ (the API
// router prefixes the route returned by WebhookPath) which accepts Amazon SNS
// HTTP(S) delivery messages carrying CloudWatch alarm state-change notifications.
//
// # SNS message types handled
//
// SNS delivers three message types, identified by the `x-amz-sns-message-type`
// HTTP header (with the JSON `Type` field as fallback):
//
//   - SubscriptionConfirmation: SNS is asking this endpoint to confirm
//     the subscription before sending real notifications. The plugin GETs the
//     provided SubscribeURL and responds 200. No records are emitted.
//   - Notification: the main case — a CloudWatch alarm state-change. The outer
//     SNS envelope's `Message` field is itself a JSON string containing the
//     CloudWatch alarm payload. The plugin decodes it and maps the alarm to a
//     snoozetypes.Record.
//   - UnsubscribeConfirmation: SNS notifying that the subscription was
//     removed. The plugin responds 200 and emits no records.
//
// # CloudWatch alarm mapping
//
//   - Source: "cloudwatch"
//   - Host: first Dimension named "InstanceId", "host", or "Host" (falls back
//     to AlarmName).
//   - Process: Trigger.MetricName (falls back to Trigger.Namespace).
//   - Severity: ALARM → "critical"; OK → "info"; INSUFFICIENT_DATA → "warning".
//   - State: OK → "close"; others → "" (firing).
//   - Message: NewStateReason (falls back to AlarmDescription).
//   - Raw: alarm fields + TopicArn from the SNS envelope.
//
// # SNS message-signature verification
//
// By default this plugin does NOT verify the SNS message signature, so
// behavior matches earlier releases and operators can rely on TopicArn and/or
// network controls (VPC, security groups, firewall rules). Setting
// config.ingest.sns_verify = true opts in to AWS SNS signature verification:
// for both SubscriptionConfirmation and Notification (and before a
// SubscribeURL is auto-confirmed) the plugin rebuilds the canonical
// string-to-sign, fetches the SigningCertURL (whose host must match
// ^sns\.[a-z0-9-]+\.amazonaws\.com(\.cn)?$ — any other host is rejected without
// a fetch), and verifies the RSA signature (SHA1 for SignatureVersion 1,
// SHA256 for 2). Any failure responds 403 and the message is not processed.
// See Notes in the documentation page.
//
// # Pipeline-submission choice
//
// internal/plugins.Host does not expose ProcessRecord directly to avoid
// pulling internal/core into the plugin contract. The plugin therefore
// runtime-asserts that the Host value also satisfies a local recordProcessor
// interface — *core.Core satisfies this shape. If the assertion fails (a
// stripped-down test host), HandleWebhook logs once and degrades to a no-op,
// matching the pattern used by internal/pluginimpl/alertmanager and grafana.
package cloudwatch

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha1" //nolint:gosec // SHA1 is mandated by AWS SNS SignatureVersion 1; it is not used as a security primitive of our choosing.
	"crypto/sha256"
	"crypto/x509"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// snsCertHostRe matches a legitimate AWS SNS signing-certificate host. Any
// SigningCertURL whose host fails this check is rejected before any fetch, to
// prevent the verifier being pointed at an attacker-controlled URL (SSRF).
var snsCertHostRe = regexp.MustCompile(`^sns\.[a-z0-9-]+\.amazonaws\.com(\.cn)?$`)

// metaYAML is the raw metadata.yaml content embedded at build time.
//
//go:embed metadata.yaml
var metaYAML []byte

func init() {
	plugins.Register("cloudwatch", metaYAML, factory)
}

// factory is the plugins.Factory entry-point.
func factory(meta plugins.Metadata) (plugins.Plugin, error) {
	return &Plugin{meta: meta}, nil
}

// recordProcessor is the slice of the alert pipeline this plugin needs. The
// concrete *core.Core satisfies this shape; the assertion sidesteps an import
// cycle through internal/plugins.Host.
type recordProcessor interface {
	ProcessRecord(ctx context.Context, rec snoozetypes.Record) (snoozetypes.Record, plugins.Action, error)
}

const confirmTimeout = 10 * time.Second

// Plugin is the CloudWatch/SNS webhook receiver.
//
// Lifecycle: Register → factory → PostInit (captures the host) → HandleWebhook
// per incoming POST. There is no persistent state to load or reload.
type Plugin struct {
	meta plugins.Metadata
	host plugins.Host

	// confirmClient returns the http.Client used to GET the SNS SubscribeURL
	// when confirming a SubscriptionConfirmation. Overridable from tests so
	// httptest servers can intercept the outbound confirmation GET.
	// When nil, a default 10 s timeout client is used.
	confirmClient func() *http.Client

	// certFetcher fetches the SNS signing certificate (PEM) from a validated
	// SigningCertURL. Overridable from tests so an in-memory PEM can be used
	// without network access. When nil, a default HTTP GET via a 10 s client
	// is used. Only called after the URL host passes snsCertHostRe.
	certFetcher func(ctx context.Context, url string) ([]byte, error)

	// warnedNoProcessor tracks whether we have already logged the "host does
	// not satisfy recordProcessor" warning, so the warning fires once per
	// process even when many webhook calls flow through.
	warnedNoProcessor atomic.Bool
}

// Name returns the registry key.
func (p *Plugin) Name() string { return p.meta.Name }

// Metadata returns the parsed metadata.yaml.
func (p *Plugin) Metadata() plugins.Metadata { return p.meta }

// PostInit wires the host in. There is no initial state to load.
func (p *Plugin) PostInit(_ context.Context, host plugins.Host) error {
	p.host = host
	return nil
}

// Reload is a no-op: the plugin has no cached state.
func (p *Plugin) Reload(_ context.Context) error { return nil }

// WebhookPath returns the route fragment mounted under /api/v1/webhook/.
// The full external URL is therefore /api/v1/webhook/cloudwatch.
func (p *Plugin) WebhookPath() string { return "/cloudwatch" }

// -- SNS outer envelope types ------------------------------------------------

// snsEnvelope is the outer SNS HTTP delivery message.
// `Message` is a raw JSON string for Notification; for other types it may be
// a plain string.
type snsEnvelope struct {
	Type           string `json:"Type"`
	MessageID      string `json:"MessageId"`
	TopicArn       string `json:"TopicArn"`
	Subject        string `json:"Subject"`
	Message        string `json:"Message"`
	Timestamp      string `json:"Timestamp"`
	SubscribeURL   string `json:"SubscribeURL"`
	UnsubscribeURL string `json:"UnsubscribeURL"`

	// Token is present on SubscriptionConfirmation / UnsubscribeConfirmation and
	// participates in their canonical string-to-sign.
	Token string `json:"Token"`

	// Signature fields used by opt-in SNS message-signature verification.
	SignatureVersion string `json:"SignatureVersion"`
	Signature        string `json:"Signature"`
	SigningCertURL   string `json:"SigningCertURL"`
}

// -- CloudWatch alarm inner payload ------------------------------------------

// cwDimension is one {name, value} entry in the Trigger.Dimensions array.
type cwDimension struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// cwTrigger holds the metric/dimensions that produced the alarm.
type cwTrigger struct {
	Namespace  string        `json:"Namespace"`
	MetricName string        `json:"MetricName"`
	Dimensions []cwDimension `json:"Dimensions"`
}

// cwAlarm is the CloudWatch alarm JSON that SNS embeds in `Message`.
type cwAlarm struct {
	AlarmName        string    `json:"AlarmName"`
	AlarmDescription string    `json:"AlarmDescription"`
	NewStateValue    string    `json:"NewStateValue"` // ALARM | OK | INSUFFICIENT_DATA
	NewStateReason   string    `json:"NewStateReason"`
	Region           string    `json:"Region"`
	StateChangeTime  string    `json:"StateChangeTime"`
	Trigger          cwTrigger `json:"Trigger"`
}

// -- HandleWebhook -----------------------------------------------------------

// HandleWebhook decodes the SNS outer envelope, handles the three SNS message
// types, and for Notification messages maps the inner CloudWatch alarm to a
// snoozetypes.Record that is submitted to the pipeline.
func (p *Plugin) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	dec := json.NewDecoder(r.Body)
	dec.UseNumber()
	var env snsEnvelope
	if err := dec.Decode(&env); err != nil {
		http.Error(w, fmt.Sprintf("invalid SNS payload: %v", err), http.StatusBadRequest)
		return
	}

	// Determine message type: prefer the header, fall back to the JSON field.
	msgType := r.Header.Get("x-amz-sns-message-type")
	if msgType == "" {
		msgType = env.Type
	}

	// Opt-in SNS message-signature verification. When Ingest.SNSVerify is off
	// (the default) this is a no-op — no cert fetch, no verification — and
	// behavior is identical to today. When on, verify BEFORE processing (and,
	// crucially, before auto-confirming a SubscribeURL).
	if p.snsVerifyEnabled() {
		// The canonical string-to-sign uses the JSON Type field, not the header.
		if err := p.verifySNSSignature(r.Context(), env, env.Type); err != nil {
			if lg := p.logger(); lg != nil {
				lg.Warn("cloudwatch: SNS signature verification failed",
					"topicArn", env.TopicArn,
					"type", env.Type,
					"err", err)
			}
			http.Error(w, "SNS signature verification failed", http.StatusForbidden)
			return
		}
	}

	switch msgType {
	case "SubscriptionConfirmation":
		p.handleSubscriptionConfirmation(w, env)
	case "Notification":
		p.handleNotification(w, r, env)
	default:
		// UnsubscribeConfirmation or unknown — acknowledge and do nothing.
		w.WriteHeader(http.StatusOK)
	}
}

// handleSubscriptionConfirmation GETs the SubscribeURL to confirm the SNS
// subscription, then responds 200. No records are emitted.
func (p *Plugin) handleSubscriptionConfirmation(w http.ResponseWriter, env snsEnvelope) {
	if env.SubscribeURL == "" {
		http.Error(w, "SubscriptionConfirmation missing SubscribeURL", http.StatusBadRequest)
		return
	}
	client := p.getConfirmClient()
	resp, err := client.Get(env.SubscribeURL) //nolint:noctx
	if err != nil {
		if lg := p.logger(); lg != nil {
			lg.Warn("cloudwatch: failed to confirm SNS subscription",
				"subscribeURL", env.SubscribeURL,
				"err", err)
		}
		http.Error(w, fmt.Sprintf("subscription confirmation failed: %v", err), http.StatusBadGateway)
		return
	}
	_ = resp.Body.Close()
	if lg := p.logger(); lg != nil {
		lg.Info("cloudwatch: SNS subscription confirmed",
			"topicArn", env.TopicArn,
			"status", resp.StatusCode)
	}
	w.WriteHeader(http.StatusOK)
}

// handleNotification parses the inner CloudWatch alarm JSON from the SNS
// Message field, builds a snoozetypes.Record, and submits it to the pipeline.
func (p *Plugin) handleNotification(w http.ResponseWriter, r *http.Request, env snsEnvelope) {
	rec := buildRecord(env)

	proc := p.recordProcessor()
	if proc == nil && !p.warnedNoProcessor.Swap(true) {
		if lg := p.logger(); lg != nil {
			lg.Warn("cloudwatch: host does not satisfy recordProcessor; webhook is a no-op",
				"plugin", p.Name())
		}
	}

	accepted := 0
	if proc != nil {
		if _, _, err := proc.ProcessRecord(r.Context(), rec); err != nil {
			if lg := p.logger(); lg != nil {
				lg.Warn("cloudwatch: pipeline rejected record",
					"plugin", p.Name(),
					"host", rec.Host,
					"process", rec.Process,
					"err", err)
			}
		} else {
			accepted++
		}
	} else {
		// No pipeline — still count the record as accepted (no-op success).
		accepted++
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":   "ok",
		"received": 1,
		"accepted": accepted,
	})
}

// buildRecord maps an SNS Notification envelope (with embedded CloudWatch
// alarm JSON in Message) to a snoozetypes.Record. If Message is not valid
// CloudWatch alarm JSON, a minimal record is built from Subject/Message.
func buildRecord(env snsEnvelope) snoozetypes.Record {
	var alarm cwAlarm
	if err := json.Unmarshal([]byte(env.Message), &alarm); err != nil || alarm.AlarmName == "" {
		// Message is not a valid CloudWatch alarm JSON — build minimal record.
		msg := env.Message
		if msg == "" {
			msg = env.Subject
		}
		return snoozetypes.Record{
			Source:    "cloudwatch",
			Host:      env.TopicArn,
			Process:   "sns",
			Severity:  "critical",
			Message:   msg,
			Timestamp: time.Now().UTC(),
			Raw: map[string]any{
				"topicArn": env.TopicArn,
				"subject":  env.Subject,
				"message":  env.Message,
			},
		}
	}
	return mapAlarmToRecord(alarm, env.TopicArn)
}

// mapAlarmToRecord converts a parsed CloudWatch alarm + TopicArn into a
// snoozetypes.Record following the mapping rules in the package doc.
func mapAlarmToRecord(alarm cwAlarm, topicArn string) snoozetypes.Record {
	// Host: first Dimension named InstanceId, host, or Host; fallback AlarmName.
	host := dimensionValue(alarm.Trigger.Dimensions, "InstanceId", "host", "Host")
	if host == "" {
		host = alarm.AlarmName
	}

	// Process: MetricName then Namespace.
	process := alarm.Trigger.MetricName
	if process == "" {
		process = alarm.Trigger.Namespace
	}

	// Severity + State mapped from NewStateValue.
	severity, state := mapState(alarm.NewStateValue)

	// Message: NewStateReason then AlarmDescription.
	msg := alarm.NewStateReason
	if msg == "" {
		msg = alarm.AlarmDescription
	}

	// Build Raw from alarm fields + TopicArn.
	raw := buildRaw(alarm, topicArn)

	return snoozetypes.Record{
		Source:    "cloudwatch",
		Host:      host,
		Process:   process,
		Severity:  severity,
		State:     state,
		Message:   msg,
		Timestamp: time.Now().UTC(),
		Raw:       raw,
	}
}

// mapState converts a CloudWatch NewStateValue to a (severity, state) pair.
//
//   - ALARM              → ("critical", "")
//   - OK                 → ("info",     "close")
//   - INSUFFICIENT_DATA  → ("warning",  "")
//   - unknown            → ("critical", "")
func mapState(newStateValue string) (severity, state string) {
	switch newStateValue {
	case "OK":
		return "info", "close"
	case "INSUFFICIENT_DATA":
		return "warning", ""
	default: // "ALARM" or unknown
		return "critical", ""
	}
}

// dimensionValue searches Dimensions for the first entry whose name matches
// one of the supplied candidates (in order) and returns its value.
func dimensionValue(dims []cwDimension, candidates ...string) string {
	for _, name := range candidates {
		for _, d := range dims {
			if d.Name == name {
				return d.Value
			}
		}
	}
	return ""
}

// buildRaw composes the Record.Raw map from the alarm fields and TopicArn.
func buildRaw(alarm cwAlarm, topicArn string) map[string]any {
	raw := map[string]any{
		"alarmName":     alarm.AlarmName,
		"newStateValue": alarm.NewStateValue,
	}
	if alarm.AlarmDescription != "" {
		raw["alarmDescription"] = alarm.AlarmDescription
	}
	if alarm.NewStateReason != "" {
		raw["newStateReason"] = alarm.NewStateReason
	}
	if alarm.Region != "" {
		raw["region"] = alarm.Region
	}
	if alarm.StateChangeTime != "" {
		raw["stateChangeTime"] = alarm.StateChangeTime
	}
	if alarm.Trigger.Namespace != "" {
		raw["namespace"] = alarm.Trigger.Namespace
	}
	if alarm.Trigger.MetricName != "" {
		raw["metricName"] = alarm.Trigger.MetricName
	}
	if len(alarm.Trigger.Dimensions) > 0 {
		dims := make([]map[string]string, 0, len(alarm.Trigger.Dimensions))
		for _, d := range alarm.Trigger.Dimensions {
			dims = append(dims, map[string]string{"name": d.Name, "value": d.Value})
		}
		raw["dimensions"] = dims
	}
	if topicArn != "" {
		raw["topicArn"] = topicArn
	}
	return raw
}

// getConfirmClient returns the http.Client to use for SNS subscription
// confirmation GETs. Uses the overridable confirmClient field; defaults to a
// new client with a 10 s timeout.
func (p *Plugin) getConfirmClient() *http.Client {
	if p.confirmClient != nil {
		return p.confirmClient()
	}
	return &http.Client{Timeout: confirmTimeout}
}

// snsVerifyEnabled reports whether opt-in SNS signature verification is on.
func (p *Plugin) snsVerifyEnabled() bool {
	if p.host == nil {
		return false
	}
	cfg := p.host.Config()
	if cfg == nil {
		return false
	}
	return cfg.Ingest.SNSVerify
}

// verifySNSSignature validates the SNS message signature per the AWS SNS spec.
//
// Steps:
//  1. Build the canonical string-to-sign for the message type (msgType).
//  2. base64-decode the Signature.
//  3. Validate SigningCertURL's host against snsCertHostRe; reject (never
//     fetch) a non-AWS host.
//  4. Fetch + parse the PEM x509 cert and take its RSA public key.
//  5. rsa.VerifyPKCS1v15 with SHA1 (SignatureVersion "1") or SHA256 ("2").
//
// Any failure returns a non-nil error; the caller responds 403 and does not
// process the message.
func (p *Plugin) verifySNSSignature(ctx context.Context, env snsEnvelope, msgType string) error {
	canonical, err := snsCanonicalString(env, msgType)
	if err != nil {
		return err
	}

	if env.Signature == "" {
		return fmt.Errorf("missing Signature")
	}
	sig, err := base64.StdEncoding.DecodeString(env.Signature)
	if err != nil {
		return fmt.Errorf("decode Signature: %w", err)
	}

	if env.SigningCertURL == "" {
		return fmt.Errorf("missing SigningCertURL")
	}
	u, err := url.Parse(env.SigningCertURL)
	if err != nil {
		return fmt.Errorf("parse SigningCertURL: %w", err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("SigningCertURL must be https, got %q", u.Scheme)
	}
	if !snsCertHostRe.MatchString(u.Hostname()) {
		// SECURITY: never fetch a non-AWS URL.
		return fmt.Errorf("SigningCertURL host %q is not an AWS SNS host", u.Hostname())
	}

	pemBytes, err := p.fetchCert(ctx, env.SigningCertURL)
	if err != nil {
		return fmt.Errorf("fetch signing cert: %w", err)
	}
	pubKey, err := rsaPublicKeyFromPEM(pemBytes)
	if err != nil {
		return err
	}

	hashAlg, digest, err := snsDigest(env.SignatureVersion, canonical)
	if err != nil {
		return err
	}
	if err := rsa.VerifyPKCS1v15(pubKey, hashAlg, digest, sig); err != nil {
		return fmt.Errorf("signature mismatch: %w", err)
	}
	return nil
}

// snsCanonicalString builds the AWS SNS canonical string-to-sign. For
// Notification the keys (in order) are Message, MessageId, Subject (only when
// present), Timestamp, TopicArn, Type. For SubscriptionConfirmation /
// UnsubscribeConfirmation they are Message, MessageId, SubscribeURL, Timestamp,
// Token, TopicArn, Type. Each is emitted as "key\nvalue\n".
func snsCanonicalString(env snsEnvelope, msgType string) (string, error) {
	var b strings.Builder
	write := func(key, value string) {
		b.WriteString(key)
		b.WriteByte('\n')
		b.WriteString(value)
		b.WriteByte('\n')
	}
	switch msgType {
	case "Notification":
		write("Message", env.Message)
		write("MessageId", env.MessageID)
		if env.Subject != "" {
			write("Subject", env.Subject)
		}
		write("Timestamp", env.Timestamp)
		write("TopicArn", env.TopicArn)
		write("Type", env.Type)
	case "SubscriptionConfirmation", "UnsubscribeConfirmation":
		write("Message", env.Message)
		write("MessageId", env.MessageID)
		write("SubscribeURL", env.SubscribeURL)
		write("Timestamp", env.Timestamp)
		write("Token", env.Token)
		write("TopicArn", env.TopicArn)
		write("Type", env.Type)
	default:
		return "", fmt.Errorf("cannot verify unknown SNS message type %q", msgType)
	}
	return b.String(), nil
}

// snsDigest returns the hash algorithm and digest for the given SNS
// SignatureVersion: "1" → SHA1, "2" → SHA256.
func snsDigest(version, canonical string) (crypto.Hash, []byte, error) {
	switch version {
	case "1":
		sum := sha1.Sum([]byte(canonical)) //nolint:gosec // SNS v1 mandates SHA1.
		return crypto.SHA1, sum[:], nil
	case "2":
		sum := sha256.Sum256([]byte(canonical))
		return crypto.SHA256, sum[:], nil
	default:
		return 0, nil, fmt.Errorf("unsupported SignatureVersion %q", version)
	}
}

// rsaPublicKeyFromPEM parses a PEM-encoded x509 certificate and returns its
// RSA public key.
func rsaPublicKeyFromPEM(pemBytes []byte) (*rsa.PublicKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("signing cert is not valid PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse signing cert: %w", err)
	}
	pub, ok := cert.PublicKey.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("signing cert public key is not RSA")
	}
	return pub, nil
}

// fetchCert retrieves the signing certificate PEM via the overridable
// certFetcher, defaulting to an HTTP GET through a 10 s client. The caller has
// already validated url against snsCertHostRe.
func (p *Plugin) fetchCert(ctx context.Context, certURL string) ([]byte, error) {
	if p.certFetcher != nil {
		return p.certFetcher(ctx, certURL)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, certURL, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: confirmTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("signing cert fetch returned status %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// recordProcessor returns the host cast to the recordProcessor contract, or
// nil if the host is missing or does not satisfy it.
func (p *Plugin) recordProcessor() recordProcessor {
	if p.host == nil {
		return nil
	}
	rp, ok := any(p.host).(recordProcessor)
	if !ok {
		return nil
	}
	return rp
}

// logger returns the host logger or nil if unavailable.
func (p *Plugin) logger() interface {
	Warn(string, ...any)
	Info(string, ...any)
} {
	if p.host == nil {
		return nil
	}
	lg := p.host.Logger()
	if lg == nil {
		return nil
	}
	return lg
}

// Compile-time proof we satisfy the contract.
var _ plugins.WebhookReceiver = (*Plugin)(nil)
