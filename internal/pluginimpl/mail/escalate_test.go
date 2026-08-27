package mail

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

func escRecord() snoozetypes.Record {
	return snoozetypes.Record{UID: "rec-1", Hash: "abc123", Host: "db-1", Message: "disk full"}
}

func escCfg() smtpConfig {
	return smtpConfig{from: "snooze@example.com", priority: 3}
}

// TestDeterministicMessageID is what makes mail threading possible at all: the
// escalation must be able to reconstruct the first delivery's Message-ID from
// the alert alone, because mail offers no API to read a stored handle back.
func TestDeterministicMessageID(t *testing.T) {
	rec := escRecord()
	id1 := deterministicMessageID(rec, escCfg(), "Mail ops")
	id2 := deterministicMessageID(rec, escCfg(), "Mail ops")
	require.Equal(t, id1, id2, "the same alert+action must always produce the same id")
	require.True(t, strings.HasPrefix(id1, "<snooze-abc123-Mail"), id1)
	require.True(t, strings.HasSuffix(id1, "@example.com>"), id1)
}

// Two mail actions on one alert must thread independently.
func TestMessageIDIsScopedPerAction(t *testing.T) {
	rec := escRecord()
	require.NotEqual(t,
		deterministicMessageID(rec, escCfg(), "A"),
		deterministicMessageID(rec, escCfg(), "B"))
}

func TestMessageIDFallsBackToUIDAndDomain(t *testing.T) {
	rec := escRecord()
	rec.Hash = ""
	id := deterministicMessageID(rec, smtpConfig{}, "")
	require.Equal(t, "<snooze-rec-1@snooze.local>", id)

	// No stable identity at all → no threading rather than a guess.
	require.Empty(t, deterministicMessageID(snoozetypes.Record{}, escCfg(), "A"))
}

// Two messages must never share a Message-ID, so an escalation derives its own
// from the root's while still pointing at it.
func TestEscalationMessageIDIsUnique(t *testing.T) {
	root := "<snooze-abc123@example.com>"
	first := escalationMessageID(root, 1)
	second := escalationMessageID(root, 2)
	require.NotEqual(t, root, first)
	require.NotEqual(t, first, second)
	require.True(t, strings.HasSuffix(first, "@example.com>"))
	require.Empty(t, escalationMessageID("not-an-id", 1))
}

func TestMailThreadFirstDelivery(t *testing.T) {
	th := mailThreadFor(escRecord(), escCfg(), plugins.NotificationPayload{
		Meta: map[string]any{"action_name": "Mail ops"},
	})
	require.NotEmpty(t, th.MessageID)
	require.Empty(t, th.InReplyTo, "a first delivery replies to nothing")
	require.False(t, th.Urgent)
}

func TestMailThreadEscalation(t *testing.T) {
	payload := plugins.NotificationPayload{
		Meta:       map[string]any{"action_name": "Mail ops"},
		Escalation: plugins.Escalation{Count: 2},
	}
	th := mailThreadFor(escRecord(), escCfg(), payload)
	root := deterministicMessageID(escRecord(), escCfg(), "Mail ops")
	require.Equal(t, root, th.InReplyTo, "the escalation must point at the first delivery")
	require.NotEqual(t, root, th.MessageID)
	require.True(t, th.Urgent)
}

// The wire message must carry the headers Outlook and Gmail actually thread on.
func TestBuildMessageEmitsThreadingHeaders(t *testing.T) {
	msg := string(buildMessage(escCfg(), []string{"a@x"}, nil, "Re: [ESCALATED #2] boom", "body",
		mailThread{
			MessageID: "<snooze-abc123.esc2@example.com>",
			InReplyTo: "<snooze-abc123@example.com>",
			Urgent:    true,
		}))

	require.Contains(t, msg, "Message-ID: <snooze-abc123.esc2@example.com>")
	require.Contains(t, msg, "In-Reply-To: <snooze-abc123@example.com>")
	require.Contains(t, msg, "References: <snooze-abc123@example.com>",
		"Gmail threads on References, Outlook on In-Reply-To — both are needed")
	require.Contains(t, msg, "Importance: high")
	require.Contains(t, msg, "X-Priority: 1")
}

// A first delivery must be byte-identical to the pre-threading output apart
// from its own Message-ID.
func TestBuildMessageWithoutThreadIsUnchanged(t *testing.T) {
	msg := string(buildMessage(escCfg(), []string{"a@x"}, nil, "boom", "body", mailThread{}))
	require.NotContains(t, msg, "Message-ID:")
	require.NotContains(t, msg, "In-Reply-To:")
	require.NotContains(t, msg, "Importance:")
	require.Contains(t, msg, "X-Priority: 3", "the configured priority must be untouched")
}

func TestEscalationSubject(t *testing.T) {
	require.Equal(t, "Re: [ESCALATED #3] disk full",
		escalationSubject("disk full", plugins.Escalation{Count: 3}))
	require.Equal(t, "Re: [ESCALATED] disk full",
		escalationSubject("disk full", plugins.Escalation{}))
}

func TestSanitizeMessageIDPart(t *testing.T) {
	require.Equal(t, "Mail-ops-1", sanitizeMessageIDPart("Mail ops#1"))
	require.Equal(t, "a.b_c-d", sanitizeMessageIDPart("a.b_c-d"))
}
