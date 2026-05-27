package discord

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

func sampleRecord() snoozetypes.Record {
	return snoozetypes.Record{
		UID:      "rec-1",
		Host:     "db-1.example.com",
		Source:   "syslog",
		Severity: "warning",
		Message:  "disk full",
	}
}

func newPluginForTest(t *testing.T) *Plugin {
	t.Helper()
	p, err := factory(plugins.Metadata{Name: "discord"})
	require.NoError(t, err)
	dp := p.(*Plugin)
	dp.newClient = func(timeout time.Duration) *http.Client {
		if timeout <= 0 {
			timeout = defaultTimeout
		}
		return &http.Client{Timeout: timeout}
	}
	return dp
}

// TestRegistration confirms the plugin self-registers on import.
func TestRegistration(t *testing.T) {
	require.True(t, slices.Contains(plugins.Registered(), "discord"))
}

// TestPluginInterfaceContract proves the compile-time assertion and confirms
// Name / PostInit / Reload behave correctly.
func TestPluginInterfaceContract(t *testing.T) {
	var _ plugins.Notifier = (*Plugin)(nil)
	p, err := factory(plugins.Metadata{Name: "discord"})
	require.NoError(t, err)
	require.Equal(t, "discord", p.Name())
	require.NoError(t, p.PostInit(context.Background(), nil))
	require.NoError(t, p.Reload(context.Background()))
}

// TestSendEmbed verifies that when use_embed=true (the default) the request
// hits the webhook URL and the body contains a properly structured embed.
func TestSendEmbed(t *testing.T) {
	var (
		mu       sync.Mutex
		captured struct {
			method      string
			contentType string
			body        []byte
		}
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		captured.method = r.Method
		captured.contentType = r.Header.Get("Content-Type")
		captured.body, _ = io.ReadAll(r.Body)
		mu.Unlock()
		// Discord returns 204 No Content on success.
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	p := newPluginForTest(t)
	rec := sampleRecord()
	err := p.Send(context.Background(), rec, plugins.NotificationPayload{
		Meta: map[string]any{
			"webhook_url": srv.URL + "/webhook",
			"use_embed":   true,
		},
	})
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()

	require.Equal(t, http.MethodPost, captured.method)
	require.Contains(t, captured.contentType, "application/json")

	var msg discordMessage
	require.NoError(t, json.Unmarshal(captured.body, &msg))

	require.Len(t, msg.Embeds, 1, "expected exactly one embed")
	embed := msg.Embeds[0]

	// The default message template produces "**<severity>** on <host>: <message>"
	require.Contains(t, embed.Title, rec.Host)
	require.Contains(t, embed.Description, rec.Message)

	// warning → 0xdaa038 = 14319672
	require.Equal(t, severityColor("warning"), embed.Color)
}

// TestSendPlain verifies that use_embed=false sends content text without embeds.
func TestSendPlain(t *testing.T) {
	var (
		mu      sync.Mutex
		rawBody []byte
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		rawBody, _ = io.ReadAll(r.Body)
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	p := newPluginForTest(t)
	rec := sampleRecord()
	err := p.Send(context.Background(), rec, plugins.NotificationPayload{
		Meta: map[string]any{
			"webhook_url": srv.URL + "/webhook",
			"use_embed":   false,
		},
	})
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()

	var msg discordMessage
	require.NoError(t, json.Unmarshal(rawBody, &msg))

	// Plain mode: content should be populated, embeds should be absent.
	require.NotEmpty(t, msg.Content, "content must be set in plain mode")
	require.Empty(t, msg.Embeds, "embeds must be absent in plain mode")
}

// TestSendAccepts200 verifies that HTTP 200 is also treated as success (some
// proxy or reverse proxy configurations return 200).
func TestSendAccepts200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	p := newPluginForTest(t)
	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta: map[string]any{"webhook_url": srv.URL},
	})
	require.NoError(t, err)
}

// TestSendNon2xxReturnsError verifies that a non-2xx response includes the
// status code and a (possibly truncated) body excerpt in the error.
func TestSendNon2xxReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message": "401: Unauthorized"}`))
	}))
	t.Cleanup(srv.Close)

	p := newPluginForTest(t)
	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta: map[string]any{"webhook_url": srv.URL},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "401")
	require.Contains(t, err.Error(), "Unauthorized")
}

// TestSendMissingWebhookURL verifies that an absent webhook_url is rejected.
func TestSendMissingWebhookURL(t *testing.T) {
	p := newPluginForTest(t)
	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta: map[string]any{},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "webhook_url")
}

// TestSendOptionalFields verifies that username and avatar_url appear in the
// posted JSON when provided.
func TestSendOptionalFields(t *testing.T) {
	var (
		mu      sync.Mutex
		rawBody []byte
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		rawBody, _ = io.ReadAll(r.Body)
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	p := newPluginForTest(t)
	err := p.Send(context.Background(), sampleRecord(), plugins.NotificationPayload{
		Meta: map[string]any{
			"webhook_url": srv.URL,
			"username":    "Snooze Bot",
			"avatar_url":  "https://example.com/avatar.png",
		},
	})
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()

	var msg discordMessage
	require.NoError(t, json.Unmarshal(rawBody, &msg))
	require.Equal(t, "Snooze Bot", msg.Username)
	require.Equal(t, "https://example.com/avatar.png", msg.AvatarURL)
}

// TestSeverityColors confirms the color mapping for each severity bucket.
func TestSeverityColors(t *testing.T) {
	cases := []struct {
		severity string
		color    int
	}{
		{"info", 0x36a64f},
		{"notice", 0x36a64f},
		{"debug", 0x36a64f},
		{"warning", 0xdaa038},
		{"error", 0xd00000},
		{"err", 0xd00000},
		{"critical", 0xd00000},
		{"emergency", 0xd00000},
		{"close", 0x2eb886},
		{"", 0x36a64f}, // unknown → info bucket
	}
	for _, tc := range cases {
		require.Equal(t, tc.color, severityColor(tc.severity),
			"color mismatch for severity=%q", tc.severity)
	}
}

// TestSendResolvedColor verifies that a record with State=="close" gets the
// resolved (green) color in the embed.
func TestSendResolvedColor(t *testing.T) {
	var (
		mu      sync.Mutex
		rawBody []byte
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		rawBody, _ = io.ReadAll(r.Body)
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	p := newPluginForTest(t)
	rec := sampleRecord()
	rec.State = "close"
	err := p.Send(context.Background(), rec, plugins.NotificationPayload{
		Meta: map[string]any{
			"webhook_url": srv.URL,
			"use_embed":   true,
		},
	})
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()

	var msg discordMessage
	require.NoError(t, json.Unmarshal(rawBody, &msg))
	require.Len(t, msg.Embeds, 1)
	require.Equal(t, 0x2eb886, msg.Embeds[0].Color, "resolved alert should use green")
}
