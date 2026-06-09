package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/rawbytes"
	"github.com/knadh/koanf/v2"
	yamlv3 "gopkg.in/yaml.v3"
)

const envPrefix = "SNOOZE_SERVER_"

// sectionFiles is the canonical mapping between a YAML file basename (without
// extension) and the dotted koanf path it populates. The legacy Python config
// file names are preserved so existing deployments can be migrated 1:1.
var sectionFiles = map[string]string{
	"core":          "core",
	"general":       "general",
	"housekeeper":   "housekeeping",
	"housekeeping":  "housekeeping",
	"notifications": "notification",
	"notification":  "notification",
	"ldap_auth":     "ldap",
	"ldap":          "ldap",
	"web":           "web",
	"auth":          "auth",
	"syncer":        "syncer",
	"oidc":          "oidc",
}

// Load reads every known section YAML file under basedir, layers environment
// variable overrides on top, then validates and returns the resulting Config.
//
// File discovery is conservative: only the names listed in “sectionFiles“
// are considered, and missing files fall back to defaults. Missing files are
// not an error — running snooze-server with an empty basedir should produce a
// fully-defaulted Config.
//
// Environment variables follow the pattern “SNOOZE_SERVER_<SECTION>_<KEY>“,
// where “<KEY>“ may contain underscores from the field name itself (for
// example “SNOOZE_SERVER_CORE_AUDIT_EXCLUDED_PATHS“). The loader knows about
// the legal key paths because it walks the struct tags.
func Load(basedir string) (*Config, error) {
	k := koanf.New(".")

	// 1. Seed with defaults so that env-only overrides keep a sensible base.
	defaultsBytes, err := defaultsYAML()
	if err != nil {
		return nil, fmt.Errorf("config: marshal defaults: %w", err)
	}
	if err := k.Load(rawbytes.Provider(defaultsBytes), yaml.Parser()); err != nil {
		return nil, fmt.Errorf("config: seed defaults: %w", err)
	}

	// 2. Layer the YAML files (if basedir exists).
	if basedir != "" {
		if err := loadYAMLFiles(k, basedir); err != nil {
			return nil, err
		}
	}

	// 3. Apply env-var overrides.
	if err := loadEnv(k); err != nil {
		return nil, fmt.Errorf("config: env overrides: %w", err)
	}

	cfg := &Config{BaseDir: basedir}
	if err := k.UnmarshalWithConf("", cfg, koanf.UnmarshalConf{
		Tag:           "koanf",
		DecoderConfig: decoderConfig(cfg),
	}); err != nil {
		return nil, fmt.Errorf("config: unmarshal: %w", err)
	}

	// Post-processing for fields the validator cannot express on its own.
	cfg.General.Normalize()
	if err := cfg.Core.Database.NormalizeURL(); err != nil {
		return nil, fmt.Errorf("config: normalize database url: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// loadYAMLFiles walks basedir and feeds the matching files to koanf at the
// right nested path. Files outside of the known section list are ignored.
func loadYAMLFiles(k *koanf.Koanf, basedir string) error {
	entries, err := os.ReadDir(basedir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("config: read basedir %q: %w", basedir, err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		ext := filepath.Ext(name)
		if ext != ".yaml" && ext != ".yml" {
			continue
		}
		base := strings.TrimSuffix(name, ext)
		section, ok := sectionFiles[base]
		if !ok {
			continue
		}
		path := filepath.Join(basedir, name)
		if err := k.Load(file.Provider(path), yaml.Parser(),
			koanf.WithMergeFunc(mergeUnder(section)),
		); err != nil {
			return fmt.Errorf("config: load %s: %w", path, err)
		}
	}
	return nil
}

// mergeUnder produces a koanf merge function that injects every parsed key
// under the given dotted path. We use this so per-section YAML files can stay
// flat instead of repeating the section name as their top-level key.
func mergeUnder(prefix string) func(src, dest map[string]any) error {
	return func(src, dest map[string]any) error {
		nested := map[string]any{prefix: src}
		mergeMaps(nested, dest)
		return nil
	}
}

func mergeMaps(src, dest map[string]any) {
	for k, v := range src {
		if existing, ok := dest[k]; ok {
			if em, okm := existing.(map[string]any); okm {
				if sm, okm2 := v.(map[string]any); okm2 {
					mergeMaps(sm, em)
					continue
				}
			}
		}
		dest[k] = v
	}
}

// loadEnv translates SNOOZE_SERVER_<...> variables into nested map updates and
// merges them on top of whatever YAML provided.
func loadEnv(k *koanf.Koanf) error {
	pathSet := buildPathSet(&Config{})
	cb := func(key, value string) (string, any) {
		canonical, ok := envKeyToPath(key, pathSet)
		if !ok {
			return "", nil
		}
		// Top-level fields ``DATABASE_URL`` etc. handled in NormalizeURL.
		if isListField(canonical) {
			return canonical, strings.Split(value, ",")
		}
		return canonical, value
	}
	// The legacy ``DATABASE_URL`` shortcut.
	if v := os.Getenv("DATABASE_URL"); v != "" {
		if err := k.Set("core.database.url", v); err != nil {
			return err
		}
	}
	return k.Load(env.ProviderWithValue(envPrefix, ".", cb), nil)
}

// envKeyToPath converts a raw env var name to a dotted koanf path, using the
// pre-computed pathSet to disambiguate field names that themselves contain
// underscores.
func envKeyToPath(envKey string, pathSet map[string]string) (string, bool) {
	rest := strings.TrimPrefix(envKey, envPrefix)
	if rest == envKey {
		return "", false
	}
	if path, ok := pathSet[rest]; ok {
		return path, true
	}
	return "", false
}

// buildPathSet walks the Config struct via reflection and returns a map from
// uppercase-underscore env-var keys to the matching dotted koanf path. The
// generated keys cover every nested koanf-tagged field, e.g.::
//
//	"CORE_AUDIT_EXCLUDED_PATHS" -> "core.audit_excluded_paths"
//	"CORE_SSL_CERTFILE"         -> "core.ssl.certfile"
//	"LDAP_BIND_PASSWORD"        -> "ldap.bind_password"
func buildPathSet(c *Config) map[string]string {
	out := make(map[string]string)
	walkStruct(reflect.TypeOf(*c), nil, out)
	return out
}

func walkStruct(t reflect.Type, prefix []string, out map[string]string) {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("koanf")
		if tag == "" || tag == "-" {
			continue
		}
		path := append(append([]string{}, prefix...), tag)
		// Always register the leaf path.
		out[strings.ToUpper(strings.Join(path, "_"))] = strings.Join(path, ".")
		ft := f.Type
		if ft.Kind() == reflect.Pointer {
			ft = ft.Elem()
		}
		// Only recurse into concrete struct types. Named scalar types such as
		// schema.Duration are not structs and must not be descended into.
		if ft.Kind() == reflect.Struct && ft.PkgPath() != "time" {
			walkStruct(ft, path, out)
		}
	}
}

// isListField returns true for fields whose value should be split on commas
// when supplied via an environment variable. Built statically because koanf's
// env provider does not see the destination struct.
func isListField(path string) bool {
	switch path {
	case "core.audit_excluded_paths",
		"core.process_plugins",
		"core.enabled_optional_plugins",
		"core.backup.excludes",
		"general.ok_severities",
		"oidc.scopes":
		return true
	}
	return false
}

// defaultsYAML marshals Default() into a YAML document so that it can be
// loaded as a baseline koanf layer.
func defaultsYAML() ([]byte, error) {
	d := Default()
	m := map[string]any{
		"core": map[string]any{
			"listen_addr":          d.Core.ListenAddr,
			"port":                 d.Core.Port,
			"bootstrap_db":         d.Core.BootstrapDB,
			"unix_socket":          d.Core.UnixSocket,
			"no_login":             d.Core.NoLogin,
			"audit_excluded_paths": d.Core.AuditExcludedPaths,
			"process_plugins":      d.Core.ProcessPlugins,
			"init_sleep":           d.Core.InitSleep,
			"create_root_user":     d.Core.CreateRootUser,
			"database": map[string]any{
				"type": d.Core.Database.Type,
				"path": d.Core.Database.Path,
			},
			"ssl": map[string]any{
				"enabled":  d.Core.SSL.Enabled,
				"certfile": d.Core.SSL.CertFile,
				"keyfile":  d.Core.SSL.KeyFile,
			},
			"backup": map[string]any{
				"enabled":  d.Core.Backup.Enabled,
				"path":     d.Core.Backup.Path,
				"excludes": d.Core.Backup.Excludes,
			},
			"cors": map[string]any{
				"allow_origins":     d.Core.CORS.AllowOrigins,
				"allow_credentials": d.Core.CORS.AllowCredentials,
			},
		},
		"general": map[string]any{
			"default_auth_backend": d.General.DefaultAuthBackend,
			"local_enabled":        d.General.LocalEnabled,
			"local_users_enabled":  d.General.LocalUsersEnabled,
			"metrics_enabled":      d.General.MetricsEnabled,
			"anonymous_enabled":    d.General.AnonymousEnabled,
			"anonymous_admin":      d.General.AnonymousAdmin,
			"ok_severities":        d.General.OKSeverities,
		},
		"housekeeping": map[string]any{
			"trigger_on_startup":   d.Housekeeper.TriggerOnStartup,
			"record_ttl":           d.Housekeeper.RecordTTL.String(),
			"cleanup_alert":        d.Housekeeper.CleanupAlert.String(),
			"cleanup_aggregate":    d.Housekeeper.CleanupAggregate.String(),
			"cleanup_comment":      d.Housekeeper.CleanupComment.String(),
			"cleanup_orphans":      d.Housekeeper.CleanupOrphans.String(),
			"cleanup_audit":        d.Housekeeper.CleanupAudit.String(),
			"cleanup_stats":        d.Housekeeper.CleanupStats.String(),
			"cleanup_snooze":       d.Housekeeper.CleanupSnooze.String(),
			"cleanup_notification": d.Housekeeper.CleanupNotification.String(),
		},
		"notification": map[string]any{
			"notification_freq":  d.Notification.NotificationFreq.String(),
			"notification_retry": d.Notification.NotificationRetry,
		},
		"ldap": map[string]any{
			"enabled":                d.LDAP.Enabled,
			"port":                   d.LDAP.Port,
			"email_attribute":        d.LDAP.EmailAttribute,
			"display_name_attribute": d.LDAP.DisplayNameAttribute,
			"member_attribute":       d.LDAP.MemberAttribute,
		},
		"web": map[string]any{
			"enabled": d.Web.Enabled,
			"path":    d.Web.Path,
		},
		"auth": map[string]any{
			"token_algorithm":     d.Auth.TokenAlgorithm,
			"token_lease":         d.Auth.TokenLease.String(),
			"refresh_token_lease": d.Auth.RefreshTokenLease.String(),
			"token_issuer":        d.Auth.TokenIssuer,
			"token_audience":      d.Auth.TokenAudience,
		},
		"syncer": map[string]any{
			"hostname":      d.Syncer.Hostname,
			"sync_interval": d.Syncer.SyncInterval.String(),
		},
		"oidc": map[string]any{
			"enabled":          d.OIDC.Enabled,
			"scopes":           d.OIDC.Scopes,
			"method":           d.OIDC.Method,
			"display_name":     d.OIDC.DisplayName,
			"icon":             d.OIDC.Icon,
			"roles_claim":      d.OIDC.RolesClaim,
			"groups_claim":     d.OIDC.GroupsClaim,
			"admin_role_value": d.OIDC.AdminRoleValue,
		},
	}
	return yamlv3.Marshal(m)
}
