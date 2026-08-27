// Package snoozepeer implements the "snoozepeer" Notifier: alert federation as
// a notification action. It relays the raw post-pipeline record JSON to a
// downstream Snooze/HTTP peer's POST /api/v1/alerts, with X-Snooze-Loop loop
// prevention for cyclic (hub-and-spoke / active-active) topologies.
//
// Config comes from the action's subcontent (endpoint, auth, tls_insecure,
// timeout), delivered via NotificationPayload.Meta. Loop prevention reads the
// inbound X-Snooze-Loop chain from the send context (propagated by the
// notification coordinator) and this server's syncer.hostname. Relay reuses
// webhook's single auth/HTTP-client code path rather than duplicating the
// bearer/basic/apikey schemes.
package snoozepeer

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/pluginimpl/webhook"
	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

//go:embed metadata.yaml
var metaYAML []byte

func init() {
	plugins.Register("snoozepeer", metaYAML, factory)
}

var _ plugins.Notifier = (*Plugin)(nil)

func factory(meta plugins.Metadata) (plugins.Plugin, error) {
	return &Plugin{meta: meta, Now: time.Now}, nil
}

// Plugin is the snoozepeer federation notifier.
type Plugin struct {
	meta plugins.Metadata
	host plugins.Host

	// Now stamps the X-Snooze-Relayed-At header; overridable in tests.
	Now func() time.Time
}

// Name returns the registered plugin identifier.
func (p *Plugin) Name() string { return "snoozepeer" }

// Metadata returns the static descriptor parsed from metadata.yaml.
func (p *Plugin) Metadata() plugins.Metadata { return p.meta }

// PostInit wires the host. There is no DB collection to load: config lives in
// the action's subcontent, delivered per-call via NotificationPayload.Meta.
func (p *Plugin) PostInit(_ context.Context, host plugins.Host) error {
	p.host = host
	if p.Now == nil {
		p.Now = time.Now
	}
	return nil
}

// Reload is a no-op: snoozepeer owns no collection (config lives in the action).
func (p *Plugin) Reload(_ context.Context) error { return nil }

// Send relays rec to the configured peer unless loop prevention suppresses it.
func (p *Plugin) Send(ctx context.Context, rec snoozetypes.Record, payload plugins.NotificationPayload) error {
	endpoint, _ := payload.Meta["endpoint"].(string)
	if strings.TrimSpace(endpoint) == "" {
		return errors.New("snoozepeer: endpoint not configured")
	}
	peer, _ := payload.Meta["action_name"].(string)

	chain := auth.LoopChainFrom(ctx)
	self := ""
	if p.host != nil && p.host.Config() != nil {
		self = p.host.Config().Syncer.Hostname
	}
	if self != "" && slices.Contains(chain, self) {
		return nil
	}
	if peer != "" && slices.Contains(chain, peer) {
		return nil
	}

	// MarshalRecord rather than json.Marshal: Record.Extra is `json:"-"`, and
	// the peer's own pipeline needs what lives there — the escalation context a
	// watchlist or aggregaterule escalation stamps into Extra, plus the
	// aggregaterule counters. Without it a re-escalated alert would arrive at
	// the peer looking like a first delivery, and the peer's notifiers would
	// open a second ticket for an incident already being tracked.
	body, err := plugins.MarshalRecord(rec)
	if err != nil {
		return fmt.Errorf("snoozepeer: marshal record: %w", err)
	}

	out := slices.Clone(chain)
	if self != "" {
		out = append(out, self)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("snoozepeer: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Snooze-Loop", strings.Join(out, ","))
	req.Header.Set("X-Snooze-Relayed-At", p.now().UTC().Format(time.RFC3339))

	if err := webhook.ApplyAuth(req, authFromMeta(payload.Meta)); err != nil {
		return fmt.Errorf("snoozepeer: apply auth: %w", err)
	}

	client := webhook.NewClient(webhook.Config{
		URL:         endpoint,
		TLSInsecure: boolFromMeta(payload.Meta, "tls_insecure"),
		Timeout:     durationFromMeta(payload.Meta["timeout"]),
	})
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("snoozepeer: relay POST failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	// Drain the (bounded) response body so net/http can return the connection to
	// the keep-alive pool. snoozepeer is the sole relay path and typically
	// relays many alerts to a fixed set of peers, so connection reuse matters.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("snoozepeer: peer rejected relay: HTTP %d", resp.StatusCode)
	}
	return nil
}

// now returns the plugin's clock (injected in tests, time.Now in production).
func (p *Plugin) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

// authFromMeta decodes the "auth" sub-document from Meta into a webhook.Auth
// so the relay reuses webhook's single auth code path.
func authFromMeta(meta map[string]any) webhook.Auth {
	m, ok := meta["auth"].(map[string]any)
	if !ok {
		return webhook.Auth{}
	}
	s := func(k string) string { v, _ := m[k].(string); return v }
	return webhook.Auth{
		Type:     strings.ToLower(s("type")),
		Token:    s("token"),
		Username: s("username"),
		Password: s("password"),
		APIKey:   s("api_key"),
		Header:   s("header"),
	}
}

func boolFromMeta(meta map[string]any, k string) bool {
	v, _ := meta[k].(bool)
	return v
}

// durationFromMeta interprets the stored timeout as seconds (the JSON/DB
// number form) or as a Go duration string. Zero/absent → 0 (the relay falls
// back to webhook's default timeout).
func durationFromMeta(v any) time.Duration {
	switch x := v.(type) {
	case int:
		return time.Duration(x) * time.Second
	case int64:
		return time.Duration(x) * time.Second
	case float64:
		return time.Duration(x) * time.Second
	case string:
		if d, err := time.ParseDuration(x); err == nil {
			return d
		}
	}
	return 0
}
