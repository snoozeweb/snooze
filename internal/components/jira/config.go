// Package jira implements the snooze-jira daemon: an outbound output plugin
// that creates and updates JIRA Cloud tickets from Snooze alerts.
//
// The package exposes a Daemon that owns:
//
//   - A tiny HTTP server bound to /alert. snooze-server hits it when a
//     notification fires; the body is one or more {project_key, alert, …}
//     envelopes. On a new alert the daemon creates a JIRA issue; on a
//     re-escalation it adds a comment to the existing issue and (optionally)
//     reopens it.
//   - A JIRA REST v3 client wrapper. Basic-auth with an Atlassian Cloud API
//     token; ADF for issue descriptions and comments.
//   - An optional background poller that watches JIRA for tickets transitioning
//     to a Done category and closes the corresponding Snooze record.
package jira

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// defaultPriorityMapping mirrors the Python plugin: a Snooze severity →
// JIRA priority name lookup applied when the operator did not override the
// mapping in jira.yaml.
var defaultPriorityMapping = map[string]string{
	"emergency": "Critical",
	"critical":  "High",
	"warning":   "Medium",
	"minor":     "Low",
	"info":      "Lowest",
}

// defaultLabels is the label list applied to new issues when no labels are
// provided in either the config or the inbound payload.
var defaultLabels = []string{"snooze"}

// Config is the YAML schema for /etc/snooze/jira.yaml.
//
// Required fields: Server (for the Snooze client used by the poller),
// JiraURL, JiraEmail, JiraAPIToken, ProjectKey. Everything else has a
// documented default.
type Config struct {
	// Server is the Snooze base URL ("https://snooze.example.com"). Used by
	// the poller to close records when a JIRA ticket transitions to Done.
	Server string `yaml:"server"`

	// Username / Password authenticate against the Snooze v1 /login endpoint.
	// Required when the poller is enabled and Token is empty.
	Username string `yaml:"username"`
	Password string `yaml:"password"`

	// Method selects the auth backend on Snooze. Defaults to "local".
	Method string `yaml:"method"`

	// Token, when set, short-circuits the Snooze login flow and is used as
	// the bearer token directly.
	Token string `yaml:"token"`

	// IngestToken, when set, is forwarded as `Authorization: Bearer <IngestToken>`
	// on every Snooze alert POST, bypassing the username/password login flow.
	// Use the per-tenant ingest token from the tenant registry (D4).
	IngestToken string `yaml:"ingest_token"`

	// Insecure disables TLS verification for the Snooze HTTPS client.
	Insecure bool `yaml:"insecure"`

	// JiraURL is the JIRA Cloud base URL ("https://mycompany.atlassian.net").
	// Required.
	JiraURL string `yaml:"jira_url"`

	// JiraEmail is the email address used for Basic auth against JIRA.
	// Required.
	JiraEmail string `yaml:"jira_email"`

	// JiraAPIToken is the Atlassian Cloud API token paired with JiraEmail.
	// Required. Create at https://id.atlassian.com/manage-profile/security/api-tokens.
	JiraAPIToken string `yaml:"jira_api_token"`

	// SSLVerify mirrors the Python flag; when false the JIRA REST client
	// skips TLS verification (opt-in for self-signed proxies). Defaults to true.
	SSLVerify *bool `yaml:"ssl_verify,omitempty"`

	// ProjectKey is the default JIRA project ("OPS") issues are created in.
	// Required (can be overridden per-payload via project_key).
	ProjectKey string `yaml:"project_key"`

	// IssueType is the default issue type name ("Task", "Bug", "Story").
	// Defaults to "Task".
	IssueType string `yaml:"issue_type"`

	// IssueTypeID is the default JIRA issue type ID (e.g. "10001"). When set
	// it overrides IssueType.
	IssueTypeID string `yaml:"issue_type_id"`

	// Priority is the fallback priority when the severity is not present in
	// PriorityMapping. Defaults to "Medium".
	Priority string `yaml:"priority"`

	// PriorityMapping maps Snooze severities to JIRA priority names. When
	// unset, defaultPriorityMapping is used.
	PriorityMapping map[string]string `yaml:"priority_mapping"`

	// Labels are the default labels applied to new issues. Defaults to
	// ["snooze"].
	Labels []string `yaml:"labels"`

	// SummaryTemplate is the Go-style template for the issue summary.
	// Supported variables: ${severity}, ${host}, ${source}, ${process},
	// ${message}, ${timestamp}. Defaults to "[${severity}] ${host} - ${message}".
	SummaryTemplate string `yaml:"summary_template"`

	// DescriptionTemplate is the template for the issue body. When set, it
	// replaces the default rich ADF description. Supports the SummaryTemplate
	// variables plus ${hash} and ${snooze_url}. Multi-line — each line
	// becomes an ADF paragraph.
	DescriptionTemplate string `yaml:"description_template"`

	// ExtraFields are additional JIRA fields applied to every new issue
	// (e.g. components, fixVersions).
	ExtraFields map[string]any `yaml:"extra_fields"`

	// Assignee is the default assignee — either a JIRA accountId or an email
	// address (the daemon resolves email → accountId via /user/search).
	Assignee string `yaml:"assignee"`

	// Reporter is the default reporter. Same email/accountId resolution as
	// Assignee.
	Reporter string `yaml:"reporter"`

	// CustomFields is a dict of customfield_XXXXX → value applied to every
	// new issue. Values are passed through to the JIRA API as-is.
	CustomFields map[string]any `yaml:"custom_fields"`

	// ReopenClosed, when true, transitions a JIRA ticket back to
	// ReopenStatusName on re-escalation if the ticket is currently in a Done
	// status category. Defaults to false.
	ReopenClosed bool `yaml:"reopen_closed"`

	// ReopenStatusName is the workflow status to transition closed tickets
	// back to. Defaults to "To Do".
	ReopenStatusName string `yaml:"reopen_status_name"`

	// InitialStatus, when set, transitions a freshly-created issue to the
	// given status (e.g. "In Progress").
	InitialStatus string `yaml:"initial_status"`

	// AlertHashCustomField is the JIRA custom field id (e.g.
	// "customfield_10500") used to store the Snooze record URL on each new
	// ticket. When unset the poller is disabled — there's no way to link a
	// JIRA ticket back to its Snooze record without it.
	AlertHashCustomField string `yaml:"alert_hash_custom_field"`

	// PollEnabled toggles the background poller. Defaults to true; requires
	// AlertHashCustomField.
	PollEnabled *bool `yaml:"poll_enabled,omitempty"`

	// PollInterval controls how often the poller queries JIRA. Defaults to
	// 5 minutes. Accepts Go duration syntax ("30s", "10m").
	PollInterval time.Duration `yaml:"poll_interval"`

	// PollJQL overrides the JQL query the poller issues. When empty the
	// daemon derives one from AlertHashCustomField:
	//
	//	cf[XXXXX] is not EMPTY AND statusCategory != Done
	PollJQL string `yaml:"poll_jql"`

	// PollMaxResults bounds the per-cycle JQL search. Defaults to 100.
	PollMaxResults int `yaml:"poll_max_results"`

	// ListeningAddress is the bind address for the /alert webhook receiver.
	// Defaults to "0.0.0.0".
	ListeningAddress string `yaml:"listening_address"`

	// ListeningPort is the bind port. Defaults to 5203 to match the Python
	// plugin.
	ListeningPort int `yaml:"listening_port"`

	// SnoozeURL is the Snooze Web UI origin used to build links in JIRA
	// descriptions and the alert-hash custom field value. Defaults to
	// "http://localhost:5200".
	SnoozeURL string `yaml:"snooze_url"`

	// MessageLimit caps the number of alerts processed per webhook call.
	// Defaults to 10.
	MessageLimit int `yaml:"message_limit"`

	// RequestTimeout caps a single JIRA HTTP request. Defaults to 30s.
	RequestTimeout time.Duration `yaml:"request_timeout"`

	// Debug enables debug-level logging.
	Debug bool `yaml:"debug"`
}

// ErrMissingConfig is the sentinel returned for missing required fields. The
// concrete fields are surfaced in the wrapped error message.
var ErrMissingConfig = errors.New("jira: required configuration missing")

// LoadConfig reads a YAML file at path and returns a fully-defaulted Config.
func LoadConfig(path string) (Config, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // path is operator-supplied
	if err != nil {
		return Config{}, fmt.Errorf("jira: read config %q: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return Config{}, fmt.Errorf("jira: parse config %q: %w", path, err)
	}
	return cfg.WithDefaults()
}

// WithDefaults fills in zero values with documented defaults and validates
// the required fields. It returns a copy so callers can keep the original
// for diagnostics.
func (c Config) WithDefaults() (Config, error) {
	if strings.TrimSpace(c.JiraURL) == "" {
		return c, fmt.Errorf("%w: jira_url", ErrMissingConfig)
	}
	if _, err := url.Parse(c.JiraURL); err != nil {
		return c, fmt.Errorf("jira: jira_url %q: %w", c.JiraURL, err)
	}
	c.JiraURL = strings.TrimRight(c.JiraURL, "/")
	if strings.TrimSpace(c.JiraEmail) == "" {
		return c, fmt.Errorf("%w: jira_email", ErrMissingConfig)
	}
	if strings.TrimSpace(c.JiraAPIToken) == "" {
		return c, fmt.Errorf("%w: jira_api_token", ErrMissingConfig)
	}
	if strings.TrimSpace(c.ProjectKey) == "" {
		return c, fmt.Errorf("%w: project_key", ErrMissingConfig)
	}
	if c.Method == "" {
		c.Method = "local"
	}
	if c.IssueType == "" {
		c.IssueType = "Task"
	}
	if c.Priority == "" {
		c.Priority = "Medium"
	}
	if c.PriorityMapping == nil {
		// Operators get a copy so a later mutation can't poison the global.
		c.PriorityMapping = make(map[string]string, len(defaultPriorityMapping))
		for k, v := range defaultPriorityMapping {
			c.PriorityMapping[k] = v
		}
	}
	if c.Labels == nil {
		c.Labels = append([]string(nil), defaultLabels...)
	}
	if c.SummaryTemplate == "" {
		c.SummaryTemplate = "[${severity}] ${host} - ${message}"
	}
	if c.ReopenStatusName == "" {
		c.ReopenStatusName = "To Do"
	}
	if c.PollInterval <= 0 {
		c.PollInterval = 5 * time.Minute
	}
	if c.PollMaxResults <= 0 {
		c.PollMaxResults = 100
	}
	if c.ListeningAddress == "" {
		c.ListeningAddress = "0.0.0.0"
	}
	if c.ListeningPort == 0 {
		c.ListeningPort = 5203
	}
	if c.SnoozeURL == "" {
		c.SnoozeURL = "http://localhost:5200"
	}
	c.SnoozeURL = strings.TrimRight(c.SnoozeURL, "/")
	if c.MessageLimit <= 0 {
		c.MessageLimit = 10
	}
	if c.RequestTimeout <= 0 {
		c.RequestTimeout = 30 * time.Second
	}
	if c.SSLVerify == nil {
		t := true
		c.SSLVerify = &t
	}
	if c.PollEnabled == nil {
		t := true
		c.PollEnabled = &t
	}
	return c, nil
}

// pollerWanted reports whether the operator asked for the poller and the
// configuration actually permits it. The poller is silently disabled when
// the alert hash custom field is empty — there is no way to correlate JIRA
// tickets back to Snooze records without it.
func (c Config) pollerWanted() bool {
	if c.PollEnabled != nil && !*c.PollEnabled {
		return false
	}
	return strings.TrimSpace(c.AlertHashCustomField) != ""
}

// sslVerify returns the resolved SSL verify flag. Treats a nil pointer as
// true so call sites stay readable.
func (c Config) sslVerify() bool {
	if c.SSLVerify == nil {
		return true
	}
	return *c.SSLVerify
}

// listenAddr returns the resolved "host:port" bind string.
func (c Config) listenAddr() string {
	return fmt.Sprintf("%s:%d", c.ListeningAddress, c.ListeningPort)
}
