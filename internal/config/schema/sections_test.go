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

// TestGeneralNormalize_SnoozeBySeverities verifies the suppression-bypass list
// is case-folded and trimmed alongside OKSeverities.
func TestGeneralNormalize_SnoozeBySeverities(t *testing.T) {
	g := General{SnoozeBySeverities: []string{"OK", " Success "}}
	g.Normalize()
	require.Equal(t, []string{"ok", "success"}, g.SnoozeBySeverities)
}

// TestDefaultGeneral_SnoozeBySeveritiesEmpty pins the default: the bypass list
// is empty (nil) so suppression bypass is off until an operator opts in.
func TestDefaultGeneral_SnoozeBySeveritiesEmpty(t *testing.T) {
	require.Empty(t, DefaultGeneral().SnoozeBySeverities)
}

func TestHousekeeper_Defaults(t *testing.T) {
	h := DefaultHousekeeper()
	require.Equal(t, 48*time.Hour, h.RecordTTL.AsDuration())
	require.Equal(t, 5*time.Minute, h.CleanupAlert.AsDuration())
	require.True(t, h.TriggerOnStartup)
	require.Equal(t, 400*24*time.Hour, h.CleanupStats.AsDuration())
}

// TestDefaultHousekeeper_LifecycleTimeouts pins the timed-alert-lifecycle
// tunables: an ack expires after 24h by default, and auto-escalation is OFF
// (escalate_after == 0) unless an operator opts in.
func TestDefaultHousekeeper_LifecycleTimeouts(t *testing.T) {
	h := DefaultHousekeeper()
	require.Equal(t, 24*time.Hour, h.AckTimeout.AsDuration())
	require.Equal(t, time.Duration(0), h.EscalateAfter.AsDuration())
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
	require.Equal(t, "/var/lib/snooze/web", w.Path)
}

func TestAuth_Defaults(t *testing.T) {
	a := DefaultAuth()
	require.Equal(t, "HS256", a.TokenAlgorithm)
	require.Equal(t, time.Hour, a.TokenLease.AsDuration())
	require.Equal(t, 7*24*time.Hour, a.RefreshTokenLease.AsDuration())
}

func TestSyncer_Defaults(t *testing.T) {
	s := DefaultSyncer()
	require.NotEmpty(t, s.Hostname)
	require.Equal(t, time.Second, s.SyncInterval.AsDuration())
}

// TestIngestDefault_AllowIsTrue locks in that fresh deployments allow alert
// ingestion by default — the kill-switch is opt-in (operators flip it to false
// during a flood or maintenance window).
func TestIngestDefault_AllowIsTrue(t *testing.T) {
	require.True(t, DefaultIngest().Allow)
}
