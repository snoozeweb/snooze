package core

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/japannext/snooze/internal/auth"
	"github.com/japannext/snooze/internal/db"
)

func TestEnsureSecrets_FirstBootGenerates(t *testing.T) {
	t.Parallel()
	drv := newFakeDB()
	jwtKey, reloadToken, err := EnsureSecrets(context.Background(), drv)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(jwtKey), auth.MinSecretBytes,
		"first-boot JWT key must be at least 32 bytes")
	require.NotEmpty(t, reloadToken)

	// Verify the rows landed in the secrets collection.
	rows := drv.collections[secretsCollection]
	require.Len(t, rows, 2)
}

func TestEnsureSecrets_SecondBootReuses(t *testing.T) {
	t.Parallel()
	drv := newFakeDB()
	jwtA, reloadA, err := EnsureSecrets(context.Background(), drv)
	require.NoError(t, err)

	jwtB, reloadB, err := EnsureSecrets(context.Background(), drv)
	require.NoError(t, err)

	require.Equal(t, jwtA, jwtB, "second-boot must reuse jwt key")
	require.Equal(t, reloadA, reloadB, "second-boot must reuse reload token")
	// No additional rows.
	require.Len(t, drv.collections[secretsCollection], 2)
}

func TestEnsureSecrets_NilDriver(t *testing.T) {
	t.Parallel()
	_, _, err := EnsureSecrets(context.Background(), nil)
	require.Error(t, err)
}

func TestEnsureSecrets_PartialState(t *testing.T) {
	t.Parallel()
	drv := newFakeDB()
	// Pre-seed only the reload token.
	drv.seed(secretsCollection, db.Document{
		"type":  "secret",
		"name":  SecretReloadToken,
		"value": "preexisting-reload-token",
	})

	jwtKey, reloadToken, err := EnsureSecrets(context.Background(), drv)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(jwtKey), auth.MinSecretBytes)
	require.Equal(t, "preexisting-reload-token", reloadToken)
	require.Len(t, drv.collections[secretsCollection], 2)
}
