package teams_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/components/teams"
)

// minimal returns a Config that passes WithDefaults — used as a starting
// point for table tests that mutate one field at a time.
func minimal() teams.Config {
	return teams.Config{
		Server:       "https://snooze.example.com",
		TenantID:     "tenant",
		ClientID:     "client",
		ClientSecret: "secret",
		TeamID:       "team",
		ChannelID:    "channel",
	}
}

func TestWithDefaults_fillsDefaults(t *testing.T) {
	cfg, err := minimal().WithDefaults()
	require.NoError(t, err)
	require.Equal(t, "https://graph.microsoft.com/v1.0", cfg.GraphBase)
	require.Equal(t, "https://login.microsoftonline.com", cfg.LoginBase)
	require.Equal(t, "https://graph.microsoft.com/.default", cfg.Scope)
	require.Equal(t, "local", cfg.Method)
	require.Equal(t, "SnoozeBot", cfg.BotName)
	require.Equal(t, 10*time.Second, cfg.PollInterval)
	require.Equal(t, time.Minute, cfg.PollLookback)
	require.Equal(t, 15*time.Second, cfg.RequestTimeout)
	require.Equal(t, "delegated", cfg.AuthMode)
	require.Equal(t, "/var/lib/snooze/teams-token.json", cfg.TokenFile)
	require.NotEmpty(t, cfg.Scopes, "Scopes should default to the Python 1.x list")
	require.Contains(t, cfg.Scopes, "ChannelMessage.Send")
	require.Contains(t, cfg.Scopes, "offline_access")
}

func TestWithDefaults_requiresFields(t *testing.T) {
	cases := []struct {
		name  string
		mut   func(c *teams.Config)
		field string
	}{
		{"server", func(c *teams.Config) { c.Server = "" }, "server"},
		{"tenant", func(c *teams.Config) { c.TenantID = "" }, "tenant_id"},
		{"client_id", func(c *teams.Config) { c.ClientID = "" }, "client_id"},
		{"team_id", func(c *teams.Config) { c.TeamID = "" }, "team_id"},
		{"channel_id", func(c *teams.Config) { c.ChannelID = "" }, "channel_id"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := minimal()
			tc.mut(&c)
			_, err := c.WithDefaults()
			require.Error(t, err)
			require.ErrorIs(t, err, teams.ErrMissingConfig)
			require.Contains(t, err.Error(), tc.field)
		})
	}
}

// TestWithDefaults_clientSecretRequiredOnlyForAppMode confirms that the
// client_secret field is no longer mandatory in delegated mode (most public
// AAD apps don't have one), but still required for the legacy app-only flow.
func TestWithDefaults_clientSecretRequiredOnlyForAppMode(t *testing.T) {
	t.Run("delegated tolerates missing client_secret", func(t *testing.T) {
		c := minimal()
		c.ClientSecret = ""
		c.AuthMode = "delegated"
		_, err := c.WithDefaults()
		require.NoError(t, err)
	})
	t.Run("client_credentials requires client_secret", func(t *testing.T) {
		c := minimal()
		c.ClientSecret = ""
		c.AuthMode = "client_credentials"
		_, err := c.WithDefaults()
		require.Error(t, err)
		require.ErrorIs(t, err, teams.ErrMissingConfig)
		require.Contains(t, err.Error(), "client_secret")
	})
}

func TestWithDefaults_trimsTrailingSlash(t *testing.T) {
	c := minimal()
	c.GraphBase = "https://graph.example.com/v1.0/"
	c.LoginBase = "https://login.example.com/"
	out, err := c.WithDefaults()
	require.NoError(t, err)
	require.Equal(t, "https://graph.example.com/v1.0", out.GraphBase)
	require.Equal(t, "https://login.example.com", out.LoginBase)
}

func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "teams.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
server: https://snooze.example.com
username: bot
password: pw
tenant_id: tenant
client_id: client
client_secret: secret
team_id: team
channel_id: channel
poll_interval: 5s
`), 0o600))
	cfg, err := teams.LoadConfig(path)
	require.NoError(t, err)
	require.Equal(t, "bot", cfg.Username)
	require.Equal(t, 5*time.Second, cfg.PollInterval)
}

func TestLoadConfig_missingFile(t *testing.T) {
	_, err := teams.LoadConfig("/no/such/path.yaml")
	require.Error(t, err)
	// Wrapping is via fmt.Errorf("%w"); check we surface an os.PathError-ish
	// signal rather than a YAML decoding failure.
	require.True(t, errors.Is(err, os.ErrNotExist), "expected os.ErrNotExist, got %v", err)
}

func TestTeamsConfig_IngestTokenRoundTrips(t *testing.T) {
	cfg := teams.Config{
		Server:      "https://snooze.example/",
		TenantID:    "azure-tid",
		ClientID:    "azure-cid",
		TeamID:      "team-guid",
		ChannelID:   "chan-id",
		IngestToken: "snooze-ingest-tok",
	}
	out, err := cfg.WithDefaults()
	require.NoError(t, err)
	require.Equal(t, "snooze-ingest-tok", out.IngestToken)
}
