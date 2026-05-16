package config

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/config/schema"
)

func TestValidate_DefaultsAreValid(t *testing.T) {
	require.NoError(t, Default().Validate())
}

func TestValidate_RejectsPortOutOfRange(t *testing.T) {
	c := Default()
	c.Core.Port = 0
	require.Error(t, c.Validate())
	c.Core.Port = 70000
	require.Error(t, c.Validate())
}

func TestValidate_RejectsBadAuthBackend(t *testing.T) {
	c := Default()
	c.General.DefaultAuthBackend = "bogus"
	require.Error(t, c.Validate())
}

func TestValidate_RejectsBadTokenAlgorithm(t *testing.T) {
	c := Default()
	c.Auth.TokenAlgorithm = "RS256"
	require.Error(t, c.Validate())
}

func TestValidate_RejectsUnknownDatabaseType(t *testing.T) {
	c := Default()
	c.Core.Database.Type = "bogus"
	require.Error(t, c.Validate())
}

func TestValidate_RejectsFileBackendWithoutPath(t *testing.T) {
	c := Default()
	c.Core.Database = schema.Database{Type: "file"}
	require.Error(t, c.Validate())
}

func TestValidate_RejectsPostgresBackendWithoutConnection(t *testing.T) {
	c := Default()
	c.Core.Database = schema.Database{Type: "postgres"}
	require.Error(t, c.Validate())
}

func TestValidate_AcceptsPostgresDSN(t *testing.T) {
	c := Default()
	c.Core.Database = schema.Database{Type: "postgres", DSN: "postgres://u:p@host/db"}
	require.NoError(t, c.Validate())
}
