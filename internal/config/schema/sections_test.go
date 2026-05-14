package schema

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestGeneral_Normalize(t *testing.T) {
	g := General{OKSeverities: []string{"OK", "Success", " Warning "}}
	g.Normalize()
	require.Equal(t, []string{"ok", "success", "warning"}, g.OKSeverities)
}

func TestHousekeeper_Defaults(t *testing.T) {
	h := DefaultHousekeeper()
	require.Equal(t, 48*time.Hour, h.RecordTTL.AsDuration())
	require.Equal(t, 5*time.Minute, h.CleanupAlert.AsDuration())
	require.True(t, h.TriggerOnStartup)
}

func TestNotification_Defaults(t *testing.T) {
	n := DefaultNotification()
	require.Equal(t, time.Minute, n.NotificationFreq.AsDuration())
	require.Equal(t, 3, n.NotificationRetry)
}

func TestLDAP_Defaults(t *testing.T) {
	l := DefaultLDAP()
	require.False(t, l.Enabled)
	require.Equal(t, 636, l.Port)
	require.Equal(t, "mail", l.EmailAttribute)
}

func TestWeb_Defaults(t *testing.T) {
	w := DefaultWeb()
	require.True(t, w.Enabled)
	require.Equal(t, "/opt/snooze/web", w.Path)
}

func TestAuth_Defaults(t *testing.T) {
	a := DefaultAuth()
	require.Equal(t, "HS256", a.TokenAlgorithm)
	require.Equal(t, time.Hour, a.TokenLease.AsDuration())
}

func TestSyncer_Defaults(t *testing.T) {
	s := DefaultSyncer()
	require.NotEmpty(t, s.Hostname)
	require.Equal(t, 1, s.Total)
}
