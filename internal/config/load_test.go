package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// writeYAML drops a YAML payload at <dir>/<name>.yaml.
func writeYAML(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, name+".yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
}

func TestConfig_Empty(t *testing.T) {
	cfg, err := Load(t.TempDir())
	require.NoError(t, err)
	require.NotNil(t, cfg)
	require.NotEmpty(t, cfg.Core.ProcessPlugins)
	require.NotEmpty(t, cfg.General.OKSeverities)
}

func TestCoreConfig_Empty(t *testing.T) {
	cfg, err := Load(t.TempDir())
	require.NoError(t, err)
	require.Equal(t, "0.0.0.0", cfg.Core.ListenAddr)
	require.Equal(t, 5200, cfg.Core.Port)
}

func TestCoreConfig_Read(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, "core", `---
listen_addr: 0.0.0.0
port: 5200
bootstrap_db: true
create_root_user: true
unix_socket: /var/run/snooze/server-test.socket
no_login: false
audit_excluded_paths: ['/api/patlite', '/metrics', '/web']
ssl:
  enabled: true
  certfile: '/etc/pki/tls/certs/snooze.crt'
  keyfile: '/etc/pki/tls/private/snooze.key'
web:
  enabled: true
  path: /opt/snooze/web
process_plugins: [rule, aggregaterule, snooze, notification]
database:
  type: mongo
`)
	cfg, err := Load(dir)
	require.NoError(t, err)
	require.Equal(t, "0.0.0.0", cfg.Core.ListenAddr)
	require.Equal(t, 5200, cfg.Core.Port)
	require.True(t, cfg.Core.BootstrapDB)
	require.True(t, cfg.Core.SSL.Enabled)
	require.Equal(t, "/etc/pki/tls/certs/snooze.crt", cfg.Core.SSL.CertFile)
	require.Equal(t, "mongo", cfg.Core.Database.Type)
}

func TestDatabaseConfig_Mongo(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, "core", `---
database:
  type: mongo
  host:
    - host01
    - host02
    - host03
  port: 27017
  username: snooze
  password: secret123
  authSource: snooze
  replicaSet: rs0
  tls: true
  tlsCAFile: '/etc/pki/tls/cert.pem'
`)
	cfg, err := Load(dir)
	require.NoError(t, err)
	require.Equal(t, "mongo", cfg.Core.Database.Type)
	hosts, ok := cfg.Core.Database.Host.([]any)
	require.True(t, ok, "host should decode as []any")
	require.Equal(t, []any{"host01", "host02", "host03"}, hosts)
}

func TestDatabaseConfig_File(t *testing.T) {
	cfg, err := Load(t.TempDir())
	require.NoError(t, err)
	require.Equal(t, "file", cfg.Core.Database.Type)
}

func TestHousekeeperConfig_Empty(t *testing.T) {
	cfg, err := Load(t.TempDir())
	require.NoError(t, err)
	require.NotZero(t, cfg.Housekeeper.RecordTTL)
	require.True(t, cfg.Housekeeper.TriggerOnStartup)
}

func TestHousekeeperConfig_NumericSeconds(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, "housekeeping", `---
record_ttl: 172800.0
cleanup_alert: 300.0
cleanup_audit: 2419200.0
`)
	cfg, err := Load(dir)
	require.NoError(t, err)
	require.Equal(t, "48h0m0s", cfg.Housekeeper.RecordTTL.String())
	require.Equal(t, "5m0s", cfg.Housekeeper.CleanupAlert.String())
}

func TestGeneralConfig_Empty(t *testing.T) {
	cfg, err := Load(t.TempDir())
	require.NoError(t, err)
	require.Equal(t, "local", cfg.General.DefaultAuthBackend)
	require.True(t, cfg.General.MetricsEnabled)
}

func TestGeneralConfig_NormalizeSeverities(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, "general", `---
ok_severities: ['OK', 'Success']
`)
	cfg, err := Load(dir)
	require.NoError(t, err)
	require.Equal(t, []string{"ok", "success"}, cfg.General.OKSeverities)
}

func TestNotificationConfig_Empty(t *testing.T) {
	cfg, err := Load(t.TempDir())
	require.NoError(t, err)
	require.Equal(t, "1m0s", cfg.Notification.NotificationFreq.String())
	require.Equal(t, 3, cfg.Notification.NotificationRetry)
}

func TestLDAPConfig_Disabled(t *testing.T) {
	cfg, err := Load(t.TempDir())
	require.NoError(t, err)
	require.False(t, cfg.LDAP.Enabled)
	require.Empty(t, cfg.LDAP.BindDN)
	require.Empty(t, cfg.LDAP.BindPassword)
}

func TestLDAPConfig_EnabledRequiresFields(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, "ldap", `---
enabled: true
host: ldap.example.com
`)
	_, err := Load(dir)
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing required fields")
}

func TestLDAPConfig_FullyConfigured(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, "ldap", `---
enabled: true
host: ldap.example.com
bind_dn: cn=myuser,ou=users,dc=example,dc=com
base_dn: ou=users,dc=example,dc=com
bind_password: my-secret-password123
user_filter: '()'
`)
	cfg, err := Load(dir)
	require.NoError(t, err)
	require.True(t, cfg.LDAP.Enabled)
	require.Equal(t, "ldap.example.com", cfg.LDAP.Host)
	require.Equal(t, "my-secret-password123", cfg.LDAP.BindPassword)
}

func TestEnv_GeneralMetrics(t *testing.T) {
	t.Setenv("SNOOZE_SERVER_GENERAL_METRICS_ENABLED", "false")
	cfg, err := Load(t.TempDir())
	require.NoError(t, err)
	require.False(t, cfg.General.MetricsEnabled)
}

func TestEnv_NestedSSLCertfile(t *testing.T) {
	t.Setenv("SNOOZE_SERVER_CORE_SSL_CERTFILE", "/etc/pki/tls/certs/snooze.crt")
	// Provide the keyfile to avoid required_if firing if SSL enabled is also set.
	cfg, err := Load(t.TempDir())
	require.NoError(t, err)
	require.Equal(t, "/etc/pki/tls/certs/snooze.crt", cfg.Core.SSL.CertFile)
}

func TestEnv_DatabaseURLMongo(t *testing.T) {
	t.Setenv("DATABASE_URL", "mongodb://host01,host02,host03/snooze")
	cfg, err := Load(t.TempDir())
	require.NoError(t, err)
	require.Equal(t, "mongo", cfg.Core.Database.Type)
}

func TestEnv_DatabaseURLPostgres(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgresql://snooze:secret@db/snooze?sslmode=require")
	cfg, err := Load(t.TempDir())
	require.NoError(t, err)
	require.Equal(t, "postgres", cfg.Core.Database.Type)
	require.Equal(t, "postgresql://snooze:secret@db/snooze?sslmode=require", cfg.Core.Database.DSN)
}

func TestEnv_AuditExcludedPathsArray(t *testing.T) {
	t.Setenv("SNOOZE_SERVER_CORE_AUDIT_EXCLUDED_PATHS", "/api1,/api2")
	cfg, err := Load(t.TempDir())
	require.NoError(t, err)
	require.Equal(t, []string{"/api1", "/api2"}, cfg.Core.AuditExcludedPaths)
}

func TestLoad_RejectsBadDatabaseType(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, "core", `---
database:
  type: bogus
`)
	_, err := Load(dir)
	require.Error(t, err)
}
