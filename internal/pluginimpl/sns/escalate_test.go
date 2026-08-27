package sns

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/plugins"
)

// A subscriber (a Lambda, an SQS consumer, a filter policy) must be able to see
// the escalation without parsing the message body.
func TestEscalationAttributes(t *testing.T) {
	form := url.Values{}
	setEscalationAttributes(form, plugins.Escalation{
		Count: 3, Reason: "timeout", Actor: "alice", PreviousSeverity: "warning",
	})

	// Collect name → value across the numbered entries.
	got := map[string]string{}
	for i := 1; i <= 4; i++ {
		prefix := "MessageAttributes.entry." + itoa(i) + "."
		name := form.Get(prefix + "Name")
		if name == "" {
			continue
		}
		got[name] = form.Get(prefix + "Value.StringValue")
	}

	require.Equal(t, "3", got["escalation_count"])
	require.Equal(t, "timeout", got["escalation_reason"])
	require.Equal(t, "alice", got["escalated_by"])
	require.Equal(t, "warning", got["previous_severity"])
	require.Equal(t, "Number", form.Get("MessageAttributes.entry.1.Value.DataType"))
}

// A first delivery's payload must be unchanged.
func TestNoAttributesOnFirstDelivery(t *testing.T) {
	form := url.Values{}
	setEscalationAttributes(form, plugins.Escalation{})
	require.Empty(t, form)
}

// Empty fields are skipped rather than sent as empty attributes, which SNS
// rejects.
func TestEmptyFieldsAreSkipped(t *testing.T) {
	form := url.Values{}
	setEscalationAttributes(form, plugins.Escalation{Count: 1})
	require.Equal(t, "escalation_count", form.Get("MessageAttributes.entry.1.Name"))
	require.Empty(t, form.Get("MessageAttributes.entry.2.Name"))
}

// TestNoFifoDedupIdIsSet guards the constraint documented on
// setEscalationAttributes: a MessageDeduplicationId derived from the alert alone
// would make AWS silently DISCARD every escalation as a duplicate — no error, no
// message, no page. If FIFO support is ever added, this test must be updated to
// assert the dedup id incorporates the escalation count, not deleted.
func TestNoFifoDedupIdIsSet(t *testing.T) {
	form := url.Values{}
	setEscalationAttributes(form, plugins.Escalation{Count: 2})
	require.Empty(t, form.Get("MessageDeduplicationId"),
		"if FIFO support lands, the dedup id must include the escalation count")
	require.Empty(t, form.Get("MessageGroupId"))
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
