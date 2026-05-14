package googlechat

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseCommand(t *testing.T) {
	cases := []struct {
		raw      string
		wantVerb ActionType
		wantArgs string
	}{
		{"ack", ActionACK, ""},
		{"  /ack 1234 ", ActionACK, "1234"},
		{"acknowledge alertX", ActionACK, "alertX"},
		{"close please", ActionClose, "please"},
		{"done", ActionClose, ""},
		{"reopen", ActionOpen, ""},
		{"reescalate", ActionESC, ""},
		{"snooze 5m flapping", ActionSnooze, "5m flapping"},
		{"/help", ActionHelp, ""},
		{"help_snooze", ActionHelp, "snooze"},
		{"this is a free-form comment", ActionComment, "this is a free-form comment"},
		{"", "", ""},
		{"  ", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			got := ParseCommand(tc.raw)
			require.Equal(t, tc.wantVerb, got.Verb, "verb mismatch for %q", tc.raw)
			require.Equal(t, tc.wantArgs, got.Args, "args mismatch for %q", tc.raw)
		})
	}
}

func TestConfigDefaults(t *testing.T) {
	cfg := Config{
		Server:             "https://snooze.example.com",
		GCPProject:         "my-proj",
		PubSubSubscription: "chat-in",
	}
	c, err := cfg.WithDefaults()
	require.NoError(t, err)
	require.Equal(t, "https://snooze.example.com", c.SnoozeURL)
	require.Equal(t, "Snooze", c.BotName)
	require.NotZero(t, c.RequestTimeout)
}

func TestConfigValidation(t *testing.T) {
	cases := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{"missing server", Config{GCPProject: "p", PubSubSubscription: "s"}, "server is required"},
		{"missing project", Config{Server: "https://x", PubSubSubscription: "s"}, "gcp_project is required"},
		{"missing subscription", Config{Server: "https://x", GCPProject: "p"}, "pubsub_subscription is required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.cfg.WithDefaults()
			require.Error(t, err)
			require.True(t, strings.Contains(err.Error(), tc.wantErr), "got %q, want substring %q", err, tc.wantErr)
		})
	}
}
