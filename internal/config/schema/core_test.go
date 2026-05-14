package schema

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCore_Defaults(t *testing.T) {
	c := DefaultCore()
	require.Equal(t, "0.0.0.0", c.ListenAddr)
	require.Equal(t, 5200, c.Port)
	require.Equal(t, "file", c.Database.Type)
	require.True(t, c.BootstrapDB)
}

func TestDatabase_NormalizeURL_Mongo(t *testing.T) {
	d := Database{URL: "mongodb://host01,host02/snooze"}
	require.NoError(t, d.NormalizeURL())
	require.Equal(t, "mongo", d.Type)
	require.Equal(t, "mongodb://host01,host02/snooze", d.Host)
	require.Empty(t, d.URL)
}

func TestDatabase_NormalizeURL_Postgres(t *testing.T) {
	d := Database{URL: "postgres://u:p@host:5432/snooze"}
	require.NoError(t, d.NormalizeURL())
	require.Equal(t, "postgres", d.Type)
	require.Equal(t, "postgres://u:p@host:5432/snooze", d.DSN)
}

func TestDatabase_NormalizeURL_Empty(t *testing.T) {
	d := Database{}
	require.NoError(t, d.NormalizeURL())
	require.Empty(t, d.Type)
}

func TestDatabase_NormalizeURL_Rejects(t *testing.T) {
	d := Database{URL: "mysql://localhost"}
	require.Error(t, d.NormalizeURL())
}
