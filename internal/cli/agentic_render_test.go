package cli

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

func sampleAgentic() snoozetypes.Agentic {
	return snoozetypes.Agentic{
		RootCause: snoozetypes.RootCause{
			Summary:    "dev26 backup failed at the S3 upload",
			Detail:     strings.Repeat("The aws-s3 container exited 1 after the dump completed. ", 4),
			Scope:      "prod/dev26/cronjob/mariadb-backup",
			Evidence:   []string{"BackoffLimitExceeded job/mariadb-backup-29838956"},
			Caveats:    []string{"The failed pod's own logs are not in Loki."},
			Confidence: "medium",
		},
		RemediationPlan: snoozetypes.RemediationPlan{
			Status: "action_required",
			Steps: []snoozetypes.Step{
				{Action: "Re-run the backup now", Command: "kubectl create job --from=cronjob/mariadb-backup m1", Risk: "low", When: "now"},
				{Action: "Rescope the restart alert to exclude Job pods", Risk: "low", When: "follow_up"},
			},
			Rollback: []snoozetypes.Step{{Action: "Delete the manual job", Command: "kubectl delete job m1", Risk: "low"}},
		},
		Analysis: snoozetypes.AnalysisMeta{At: "2026-09-25T12:20:01Z", By: "snooze", Source: "alert-rca"},
	}
}

func TestRenderAgentic(t *testing.T) {
	var buf bytes.Buffer
	renderAgentic(&buf, sampleAgentic())
	out := buf.String()

	require.Contains(t, out, "Analysis    2026-09-25T12:20:01Z · by snooze · source alert-rca\n")
	require.Contains(t, out, "Verdict     [ACTION REQUIRED] · confidence medium\n")
	require.Contains(t, out, "Scope       prod/dev26/cronjob/mariadb-backup\n")
	require.Contains(t, out, "\nSummary\n  dev26 backup failed at the S3 upload\n")
	require.Less(t, strings.Index(out, "Caveats"), strings.Index(out, "Evidence"), "caveats are read before evidence")
	require.Contains(t, out, "\nNow\n  1. [low] Re-run the backup now\n           $ kubectl create job --from=cronjob/mariadb-backup m1\n")
	require.Contains(t, out, "\nFollow-up\n  1. [low] Rescope the restart alert to exclude Job pods\n")
	require.Contains(t, out, "\nRollback\n  1. [low] Delete the manual job\n")
	require.NotContains(t, out, "\nSteps\n", "every step had a when")
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "$ ") {
			continue // commands are never wrapped
		}
		require.LessOrEqual(t, len([]rune(line)), agenticWrap, line)
	}
}

// A legacy-schema plan (no `when`, no status) still renders every step.
func TestRenderAgentic_Legacy(t *testing.T) {
	a := sampleAgentic()
	a.RemediationPlan.Status = ""
	for i := range a.RemediationPlan.Steps {
		a.RemediationPlan.Steps[i].When = ""
	}
	var buf bytes.Buffer
	renderAgentic(&buf, a)
	require.Contains(t, buf.String(), "[NO VERDICT]")
	require.Contains(t, buf.String(), "\nSteps\n  1. [low] Re-run the backup now\n")
}

func TestWrapText(t *testing.T) {
	require.Equal(t, []string{"aaa bbb", "ccc"}, wrapText("aaa bbb ccc", 7))
	require.Equal(t, []string{"averyverylongword", "x"}, wrapText("averyverylongword x", 5))
	require.Equal(t, []string{"one", "two"}, wrapText("one\ntwo", 80), "explicit breaks kept")
}

// `record show` digests the analysis into one line; --json keeps it whole.
func TestRecordShowDigestsAgentic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"uid":"u-1","host":"K8S prod","agentic":{
			"root_cause":{"summary":"backup failed\nat upload","confidence":"medium"},
			"remediation_plan":{"status":"action_required","steps":[{"action":"x","risk":"low"}]}}}]}`))
	}))
	defer srv.Close()

	rt, _, _ := newTestRuntime(t, srv)
	rt.flags.Token = "tok"
	out, _, err := executeCmd(t, rt, "record", "show", "u-1")
	require.NoError(t, err)
	require.Contains(t, out,
		"agentic: action_required · confidence medium · backup failed at upload  (see: snooze record agentic get u-1)\n")

	rt2, _, _ := newTestRuntime(t, srv)
	rt2.flags.Token = "tok"
	out, _, err = executeCmd(t, rt2, "record", "show", "u-1", "--json")
	require.NoError(t, err)
	require.Contains(t, out, `"remediation_plan"`)
}
