package schema

import (
	"net"
	"net/url"
	"strings"
)

// Core is the bootstrap configuration that must be present before the server
// starts. It mirrors the Python “CoreConfig“ model but drops the fields that
// have moved to the runtime settings store.
type Core struct {
	ListenAddr         string   `koanf:"listen_addr" validate:"omitempty,ip"`
	Port               int      `koanf:"port" validate:"min=1,max=65535"`
	BootstrapDB        bool     `koanf:"bootstrap_db"`
	UnixSocket         string   `koanf:"unix_socket"`
	NoLogin            bool     `koanf:"no_login"`
	AuditExcludedPaths []string `koanf:"audit_excluded_paths"`
	ProcessPlugins     []string `koanf:"process_plugins"`
	// EnabledOptionalPlugins is the explicit allow-list of opt-in plugins that
	// would otherwise be filtered out at boot. The set of "optional" plugins
	// is defined by core.optionalPlugins (currently just `patlite`); any name
	// in this list joins the active set, anything outside it stays disabled
	// even if its package is blank-imported.
	EnabledOptionalPlugins []string `koanf:"enabled_optional_plugins"`
	Database               Database `koanf:"database" validate:"required"`
	InitSleep          int      `koanf:"init_sleep" validate:"min=0"`
	CreateRootUser     bool     `koanf:"create_root_user"`
	SSL                SSL      `koanf:"ssl"`
	Backup             Backup   `koanf:"backup"`
	CORS               CORS     `koanf:"cors"`
}

// DefaultCore returns the values used when no core.yaml file exists, matching
// the Python defaults in “snooze/utils/config.py::CoreConfig“.
func DefaultCore() Core {
	return Core{
		ListenAddr:         "0.0.0.0",
		Port:               5200,
		BootstrapDB:        true,
		UnixSocket:         "/var/run/snooze/server.socket",
		NoLogin:            false,
		AuditExcludedPaths: []string{"/api/patlite", "/metrics", "/web"},
		ProcessPlugins:     []string{"rule", "aggregaterule", "snooze", "notification"},
		Database:           Database{Type: "file", Path: "./db.json"},
		InitSleep:          5,
		CreateRootUser:     true,
		SSL:                SSL{},
		Backup:             DefaultBackup(),
		CORS:               CORS{AllowOrigins: "*", AllowCredentials: "*"},
	}
}

// Database is the polymorphic database backend selector. It accepts the same
// keys as the Python “DatabaseConfig“ union (“mongo“, “file“,
// “postgres“) plus a flat “url“ shortcut used by the env-var
// “DATABASE_URL“.
type Database struct {
	Type string `koanf:"type" validate:"oneof=mongo file postgres"`

	// File backend.
	Path string `koanf:"path"`

	// Mongo / Postgres connection knobs. Stored loosely so that we can keep
	// passing the raw keys to the driver in subsequent phases. The fields
	// below cover the cases exercised by tests; anything else goes through
	// Extra.
	Host        any    `koanf:"host"`
	Port        int    `koanf:"port"`
	Username    string `koanf:"username"`
	Password    string `koanf:"password"`
	AuthSource  string `koanf:"authSource"`
	ReplicaSet  string `koanf:"replicaSet"`
	TLS         bool   `koanf:"tls"`
	TLSCAFile   string `koanf:"tlsCAFile"`
	Database    string `koanf:"database"`
	DSN         string `koanf:"dsn"`
	SSLMode     string `koanf:"sslmode"`
	PoolMinSize int    `koanf:"pool_min_size"`
	PoolMaxSize int    `koanf:"pool_max_size"`
	URL         string `koanf:"url"`
}

// NormalizeURL turns a DATABASE_URL value into the matching typed backend.
// It is a no-op when “URL“ is empty.
func (d *Database) NormalizeURL() error {
	if d.URL == "" {
		return nil
	}
	u, err := url.Parse(d.URL)
	if err != nil {
		return err
	}
	switch strings.ToLower(u.Scheme) {
	case "mongodb", "mongo":
		d.Type = "mongo"
		d.Host = d.URL
	case "postgres", "postgresql":
		d.Type = "postgres"
		d.DSN = d.URL
	default:
		return &url.Error{Op: "parse", URL: d.URL, Err: errUnsupportedScheme(u.Scheme)}
	}
	d.URL = ""
	return nil
}

type errUnsupportedScheme string

func (e errUnsupportedScheme) Error() string { return "unsupported database scheme: " + string(e) }

// SSL toggles TLS termination for the embedded HTTP listener.
type SSL struct {
	Enabled  bool   `koanf:"enabled"`
	CertFile string `koanf:"certfile" validate:"required_if=Enabled true"`
	KeyFile  string `koanf:"keyfile" validate:"required_if=Enabled true"`
}

// Backup controls the optional backup loop.
type Backup struct {
	Enabled  bool     `koanf:"enabled"`
	Path     string   `koanf:"path"`
	Excludes []string `koanf:"excludes"`
}

// DefaultBackup mirrors the Python defaults.
func DefaultBackup() Backup {
	return Backup{
		Enabled:  true,
		Path:     "/var/lib/snooze",
		Excludes: []string{"record", "stats", "comment", "secrets", "aggregate", "system.profile"},
	}
}

// CORS controls the Access-Control-Allow-* response headers.
type CORS struct {
	AllowOrigins     string `koanf:"allow_origins"`
	AllowCredentials string `koanf:"allow_credentials"`
}

// ResolvedListenAddr returns the address as a “net.IP“. It is the validator's
// canonical accessor.
func (c Core) ResolvedListenAddr() net.IP { return net.ParseIP(c.ListenAddr) }
