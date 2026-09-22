package core

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// An inbound alert that carries a protected field has it dropped before any
// plugin runs: the alert is still processed (a sender must not be able to
// break its own alerting by guessing a protected name), it just cannot
// fabricate agent-authored analysis.
func TestProcessRecord_StripsProtectedFieldFromInboundAlert(t *testing.T) {
	t.Parallel()
	p1 := &fakeProcessor{name: "rule", result: plugins.Result{Action: plugins.ActionContinue}}
	c, drv := newPipelineCore(t, p1)

	rec := snoozetypes.Record{
		UID:     "uid-1",
		Message: "hello",
		Extra: map[string]any{
			"agentic":    map[string]any{"root_cause": map[string]any{"summary": "forged"}},
			"custom_tag": "kept",
		},
	}
	out, action, err := c.ProcessRecord(pctx(), rec)
	require.NoError(t, err)
	require.Equal(t, plugins.ActionContinue, action)

	require.NotContains(t, p1.recvRec.Extra, "agentic", "the plugin chain never sees a forged analysis")
	require.NotContains(t, out.Extra, "agentic")
	require.Equal(t, "kept", out.Extra["custom_tag"], "ordinary extra fields are untouched")
	require.Equal(t, 1, drv.writeCount(recordCollection), "the alert is still processed and stored")
}

func TestProcessRecord_NoProtectedFieldIsANoOp(t *testing.T) {
	t.Parallel()
	p1 := &fakeProcessor{name: "rule", result: plugins.Result{Action: plugins.ActionContinue}}
	c, _ := newPipelineCore(t, p1)

	out, _, err := c.ProcessRecord(pctx(), snoozetypes.Record{UID: "uid-2", Message: "clean"})
	require.NoError(t, err)
	require.Nil(t, out.Extra)
}
