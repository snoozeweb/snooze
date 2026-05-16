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
		{"client_secret", func(c *teams.Config) { c.ClientSecret = "" }, "client_secret"},
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
