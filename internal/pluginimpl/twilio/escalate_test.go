package twilio

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

func throttleRecord() snoozetypes.Record {
	return snoozetypes.Record{UID: "rec-1", Hash: "abc123", Host: "db-1", Message: "disk full"}
}

// SMS is billed and length-capped, so a re-escalation sends the short form.
func TestEscalationSMSIsShort(t *testing.T) {
	rec := throttleRecord()
	got := escalationSMS("a very long rendered original message with lots of context", rec,
		plugins.Escalation{Count: 3})
	require.Equal(t, "ESC #3 db-1: disk full", got)
}

func TestEscalationSMSLeavesFirstDeliveryAlone(t *testing.T) {
	original := "the full original message"
	require.Equal(t, original,
		escalationSMS(original, throttleRecord(), plugins.Escalation{}))
}

// The throttle is a cost guard: a flapping alert must not be able to ring a
// phone (and bill for it) on every escalation.
func TestThrottleSuppressesInsideTheWindow(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	p := &Plugin{clock: func() time.Time { return now }}
	rec := throttleRecord()

	require.False(t, p.throttled(rec, "SMS on-call", time.Minute), "the first escalation always goes")
	require.True(t, p.throttled(rec, "SMS on-call", time.Minute), "a second inside the window is suppressed")

	now = now.Add(2 * time.Minute)
	require.False(t, p.throttled(rec, "SMS on-call", time.Minute), "past the window it goes again")
}

// Throttling is per alert and per action: one noisy alert must not silence a
// different one, and two actions on one alert are independent.
func TestThrottleIsScopedPerAlertAndAction(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	p := &Plugin{clock: func() time.Time { return now }}

	a := throttleRecord()
	b := throttleRecord()
	b.Hash = "different"

	require.False(t, p.throttled(a, "act", time.Minute))
	require.False(t, p.throttled(b, "act", time.Minute), "a different alert must not be throttled")
	require.False(t, p.throttled(a, "other", time.Minute), "a different action must not be throttled")
	require.True(t, p.throttled(a, "act", time.Minute))
}

// Stale entries are pruned so the map tracks alerts actively escalating rather
// than every alert ever seen.
func TestThrottleMapIsPruned(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	p := &Plugin{clock: func() time.Time { return now }}

	for i := range 50 {
		rec := throttleRecord()
		rec.Hash = string(rune('a'+i%26)) + string(rune('a'+i/26))
		p.throttled(rec, "act", time.Minute)
	}
	require.Len(t, p.lastEscalation, 50)

	now = now.Add(time.Hour)
	p.throttled(throttleRecord(), "act", time.Minute)
	require.Len(t, p.lastEscalation, 1, "everything older than the window must be dropped")
}

// A record with no stable identity cannot be throttled — better to send than to
// suppress based on a key we cannot compute.
func TestThrottleSkipsIdentitylessRecords(t *testing.T) {
	p := &Plugin{clock: func() time.Time { return time.Unix(1, 0) }}
	require.False(t, p.throttled(snoozetypes.Record{}, "act", time.Minute))
	require.False(t, p.throttled(snoozetypes.Record{}, "act", time.Minute))
}

func TestIntFromMetaShapes(t *testing.T) {
	for name, v := range map[string]any{
		"int":     3,
		"int64":   int64(3),
		"float64": float64(3),
		"string":  "3",
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, 3, intFromMeta(map[string]any{"k": v}, "k"))
		})
	}
	require.Zero(t, intFromMeta(map[string]any{}, "k"))
	require.Zero(t, intFromMeta(map[string]any{"k": "nope"}, "k"))
	require.Zero(t, intFromMeta(map[string]any{"k": -5}, "k"), "a negative threshold means off")
}

// The knobs are off by default: dropping a page is worse than paying for one,
// so an operator has to opt in.
func TestEscalationKnobsDefaultOff(t *testing.T) {
	cfg, err := configFromMeta(map[string]any{
		"account_sid": "AC" + "0000000000000000000000000000000",
		"auth_token":  "tok",
		"from":        "+15550000000",
		"to":          "+15551111111",
	})
	require.NoError(t, err)
	require.Zero(t, cfg.EscalationThrottle)
	require.Zero(t, cfg.VoiceEscalationAt)
}
