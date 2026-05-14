package jira_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/japannext/snooze/internal/components/jira"
)

// minimal returns the smallest Config that passes WithDefaults — used as a
// starting point for table tests that mutate one field at a time.
func minimal() jira.Config {
	return jira.Config{
		JiraURL:      "https://mycompany.atlassian.net",
		JiraEmail:    "bot@mycompany.com",
		JiraAPIToken: "tok",
		ProjectKey:   "OPS",
	}
}

func TestWithDefaults_fillsDefaults(t *testing.T) {
	cfg, err := minimal().WithDefaults()
	require.NoError(t, err)
	require.Equal(t, "Task", cfg.IssueType)
	require.Equal(t, "Medium", cfg.Priority)
	require.Equal(t, "[${severity}] ${host} - ${message}", cfg.SummaryTemplate)
	require.Equal(t, "To Do", cfg.ReopenStatusName)
	require.Equal(t, 5*time.Minute, cfg.PollInterval)
	require.Equal(t, 100, cfg.PollMaxResults)
	require.Equal(t, "0.0.0.0", cfg.ListeningAddress)
	require.Equal(t, 5203, cfg.ListeningPort)
	require.Equal(t, "http://localhost:5200", cfg.SnoozeURL)
	require.Equal(t, 10, cfg.MessageLimit)
	require.Equal(t, 30*time.Second, cfg.RequestTimeout)
	require.Equal(t, []string{"snooze"}, cfg.Labels)
	require.Equal(t, "High", cfg.PriorityMapping["critical"])
	require.Equal(t, "local", cfg.Method)
}

func TestWithDefaults_requiresFields(t *testing.T) {
	cases := []struct {
		name  string
		mut   func(c *jira.Config)
		field string
	}{
		{"jira_url", func(c *jira.Config) { c.JiraURL = "" }, "jira_url"},
		{"jira_email", func(c *jira.Config) { c.JiraEmail = "" }, "jira_email"},
		{"jira_api_token", func(c *jira.Config) { c.JiraAPIToken = "" }, "jira_api_token"},
		{"project_key", func(c *jira.Config) { c.ProjectKey = "" }, "project_key"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := minimal()
			tc.mut(&c)
			_, err := c.WithDefaults()
			require.Error(t, err)
			require.ErrorIs(t, err, jira.ErrMissingConfig)
			require.Contains(t, err.Error(), tc.field)
		})
	}
}

func TestWithDefaults_trimsTrailingSlash(t *testing.T) {
	c := minimal()
	c.JiraURL = "https://my.atlassian.net/"
	c.SnoozeURL = "https://snooze.example.com/"
	out, err := c.WithDefaults()
	require.NoError(t, err)
	require.Equal(t, "https://my.atlassian.net", out.JiraURL)
	require.Equal(t, "https://snooze.example.com", out.SnoozeURL)
}

func TestWithDefaults_priorityMappingDeepCopy(t *testing.T) {
	a, err := minimal().WithDefaults()
	require.NoError(t, err)
	b, err := minimal().WithDefaults()
	require.NoError(t, err)
	a.PriorityMapping["critical"] = "Custom"
	require.Equal(t, "High", b.PriorityMapping["critical"],
		"default priority mapping must not share state across configs")
}

func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "jira.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
jira_url: https://my.atlassian.net
jira_email: bot@example.com
jira_api_token: ATATT3xFf
project_key: OPS
priority: High
priority_mapping:
  emergency: P0
  critical: P1
labels: ["snooze", "ops"]
poll_interval: 30s
listening_port: 6000
`), 0o600))
	cfg, err := jira.LoadConfig(path)
	require.NoError(t, err)
	require.Equal(t, "High", cfg.Priority)
	require.Equal(t, "P1", cfg.PriorityMapping["critical"])
	require.Equal(t, []string{"snooze", "ops"}, cfg.Labels)
	require.Equal(t, 30*time.Second, cfg.PollInterval)
	require.Equal(t, 6000, cfg.ListeningPort)
}

func TestLoadConfig_missingFile(t *testing.T) {
	_, err := jira.LoadConfig("/no/such/file.yaml")
	require.Error(t, err)
	require.True(t, errors.Is(err, os.ErrNotExist), "expected os.ErrNotExist, got %v", err)
}
