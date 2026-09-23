package snoozetypes

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// validBody is the smallest payload that satisfies the schema.
const validBody = `{
  "root_cause": {"summary": "disk filled by unrotated nginx logs", "confidence": "high"},
  "remediation_plan": {"steps": [{"action": "rotate logs", "risk": "low"}]}
}`

func TestDecodeAgenticRequestAcceptsMinimalBody(t *testing.T) {
	req, err := DecodeAgenticRequest([]byte(validBody))
	require.NoError(t, err)
	require.Equal(t, "disk filled by unrotated nginx logs", req.RootCause.Summary)
	require.Equal(t, ConfidenceHigh, req.RootCause.Confidence)
	require.Len(t, req.RemediationPlan.Steps, 1)
	require.Equal(t, RiskLow, req.RemediationPlan.Steps[0].Risk)
}

func TestDecodeAgenticRequestAcceptsFullBody(t *testing.T) {
	body := `{
      "root_cause": {
        "summary": "jwt-key-sync OOMKilled at its 64Mi limit",
        "scope": "ovh/monitoring/jwt-key-sync",
        "evidence": ["describe pod: Reason=OOMKilled", "container_memory_max_usage=67Mi"],
        "confidence": "medium"
      },
      "remediation_plan": {
        "steps": [{"action": "raise limit to 128Mi", "command": "kubectl -n monitoring set resources ...", "risk": "low"}],
        "rollback": [{"action": "revert the values.yaml bump", "risk": "low"}],
        "automatable": true
      },
      "source": "alert-rca"
    }`
	req, err := DecodeAgenticRequest([]byte(body))
	require.NoError(t, err)
	require.Len(t, req.RootCause.Evidence, 2)
	require.True(t, req.RemediationPlan.Automatable)
	require.Len(t, req.RemediationPlan.Rollback, 1)
	require.Equal(t, "alert-rca", req.Source)
}

func TestDecodeAgenticRequestRejectsUnknownField(t *testing.T) {
	body := `{"root_cause": {"summary": "x", "confidence": "high", "probability": 0.5},
              "remediation_plan": {"steps": [{"action": "a", "risk": "low"}]}}`
	_, err := DecodeAgenticRequest([]byte(body))
	var verrs ValidationErrors
	require.ErrorAs(t, err, &verrs)
	require.Equal(t, map[string]any{"probability": "unknown field"}, verrs.Details())
}

func TestDecodeAgenticRequestRejectsClientSuppliedProvenance(t *testing.T) {
	body := `{"root_cause": {"summary": "x", "confidence": "high"},
              "remediation_plan": {"steps": [{"action": "a", "risk": "low"}]},
              "analysis": {"by": "root"}}`
	_, err := DecodeAgenticRequest([]byte(body))
	var verrs ValidationErrors
	require.ErrorAs(t, err, &verrs)
	require.Contains(t, verrs.Details(), "analysis")
}

func TestDecodeAgenticRequestRejectsMalformedJSON(t *testing.T) {
	_, err := DecodeAgenticRequest([]byte(`{"root_cause":`))
	require.Error(t, err)
	var verrs ValidationErrors
	require.False(t, len(err.Error()) == 0)
	require.NotErrorAs(t, err, &verrs, "a syntax error is not a schema violation")
}

func TestValidateReportsEveryFailureAtOnce(t *testing.T) {
	req := AgenticRequest{
		RootCause:       RootCause{Confidence: "certain"},
		RemediationPlan: RemediationPlan{},
	}
	details := req.Validate().Details()
	require.Equal(t, "is required", details["root_cause.summary"])
	require.Equal(t, "must be one of high|medium|low", details["root_cause.confidence"])
	require.Equal(t, "must hold at least one step", details["remediation_plan.steps"])
}

func TestValidateStepFields(t *testing.T) {
	req := AgenticRequest{
		RootCause: RootCause{Summary: "s", Confidence: ConfidenceLow},
		RemediationPlan: RemediationPlan{
			Steps:    []Step{{Action: "  ", Risk: "catastrophic"}},
			Rollback: []Step{{Action: "undo"}},
		},
	}
	details := req.Validate().Details()
	require.Equal(t, "is required", details["remediation_plan.steps[0].action"])
	require.Equal(t, "must be one of low|medium|high", details["remediation_plan.steps[0].risk"])
	require.Equal(t, "is required (low|medium|high)", details["remediation_plan.rollback[0].risk"])
}

func TestValidateEnforcesSizeLimits(t *testing.T) {
	long := make([]byte, MaxSummaryLen+1)
	for i := range long {
		long[i] = 'a'
	}
	evidence := make([]string, MaxEvidenceItems+1)
	for i := range evidence {
		evidence[i] = "e"
	}
	req := AgenticRequest{
		RootCause: RootCause{Summary: string(long), Confidence: ConfidenceHigh, Evidence: evidence},
		RemediationPlan: RemediationPlan{
			Steps: []Step{{Action: "a", Risk: RiskLow, Command: string(make([]byte, MaxCommandLen+1))}},
		},
		Source: string(long),
	}
	details := req.Validate().Details()
	require.Contains(t, details, "root_cause.summary")
	require.Contains(t, details, "root_cause.evidence")
	require.Contains(t, details, "remediation_plan.steps[0].command")
	require.Contains(t, details, "source")
}

func TestValidateRejectsEmptyEvidenceItem(t *testing.T) {
	req := AgenticRequest{
		RootCause:       RootCause{Summary: "s", Confidence: ConfidenceHigh, Evidence: []string{"ok", "   "}},
		RemediationPlan: RemediationPlan{Steps: []Step{{Action: "a", Risk: RiskLow}}},
	}
	require.Equal(t, "must not be empty", req.Validate().Details()["root_cause.evidence[1]"])
}

func TestValidateRejectsTooManySteps(t *testing.T) {
	steps := make([]Step, MaxSteps+1)
	for i := range steps {
		steps[i] = Step{Action: "a", Risk: RiskLow}
	}
	req := AgenticRequest{
		RootCause:       RootCause{Summary: "s", Confidence: ConfidenceHigh},
		RemediationPlan: RemediationPlan{Steps: steps},
	}
	require.Equal(t, "must hold at most 20 steps", req.Validate().Details()["remediation_plan.steps"])
}

func TestToAgenticStampsProvenanceInUTC(t *testing.T) {
	req, err := DecodeAgenticRequest([]byte(validBody))
	require.NoError(t, err)
	req.Source = "alert-rca"

	paris, err := time.LoadLocation("Europe/Paris")
	require.NoError(t, err)
	at := time.Date(2026, 9, 21, 11, 40, 0, 0, paris)

	ag := req.ToAgentic("agent-bot", at)
	require.Equal(t, "2026-09-21T09:40:00Z", ag.Analysis.At)
	require.Equal(t, "agent-bot", ag.Analysis.By)
	require.Equal(t, "alert-rca", ag.Analysis.Source)
	require.Equal(t, req.RootCause, ag.RootCause)
}

func TestAgenticDocumentRoundTrip(t *testing.T) {
	req, err := DecodeAgenticRequest([]byte(validBody))
	require.NoError(t, err)
	doc, err := req.ToAgentic("tester", time.Unix(0, 0)).Document()
	require.NoError(t, err)

	rc, ok := doc["root_cause"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "high", rc["confidence"])
	// Optional empty fields stay out of the stored document.
	require.NotContains(t, rc, "scope")
	require.NotContains(t, rc, "evidence")

	analysis, ok := doc["analysis"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "tester", analysis["by"])
}

// Limits count characters, not bytes: an accented summary well under the
// character cap must not be rejected for its UTF-8 encoding.
func TestValidateCountsRunesNotBytes(t *testing.T) {
	// 400 two-byte runes: 400 characters, 800 bytes.
	accented := strings.Repeat("é", 400)
	req := AgenticRequest{
		RootCause: RootCause{Summary: accented, Confidence: ConfidenceHigh,
			Scope: strings.Repeat("ü", 150), Evidence: []string{strings.Repeat("à", 400)}},
		RemediationPlan: RemediationPlan{Steps: []Step{{
			Action: accented, Command: strings.Repeat("ç", 900), Risk: RiskLow,
		}}},
	}
	require.Empty(t, req.Validate(), "a 400-character summary is inside the 500-character limit")

	// And the cap still bites one rune past it.
	req.RootCause.Summary = strings.Repeat("é", MaxSummaryLen+1)
	require.Equal(t, "must be at most 500 characters",
		req.Validate().Details()["root_cause.summary"])
}

// Trailing data is a sign the caller concatenated or double-wrote the body;
// accepting it silently stores only the first object and reports success.
func TestDecodeAgenticRequestRejectsTrailingData(t *testing.T) {
	for name, suffix := range map[string]string{
		"garbage":       ` garbage {"analysis":1}`,
		"second object": `{"root_cause":{"summary":"other","confidence":"low"}}`,
		"bare token":    `null`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := DecodeAgenticRequest([]byte(validBody + suffix))
			var verrs ValidationErrors
			require.ErrorAs(t, err, &verrs)
			require.Equal(t,
				map[string]any{"$": "unexpected data after the JSON object"},
				verrs.Details())
		})
	}
}

func TestDecodeAgenticRequestAcceptsTrailingWhitespace(t *testing.T) {
	_, err := DecodeAgenticRequest([]byte(validBody + "\n\t \r\n"))
	require.NoError(t, err)
}

// A wrong JSON type is a schema violation the caller can fix from the path,
// not an undecodable body — it must come back keyed, like every other one.
func TestDecodeAgenticRequestReportsTypeMismatchAsFieldError(t *testing.T) {
	cases := map[string]struct {
		body string
		path string
		want string
	}{
		"scalar for enum": {
			body: `{"root_cause": {"summary": "s", "confidence": 1},
			        "remediation_plan": {"steps": [{"action": "a", "risk": "low"}]}}`,
			path: "root_cause.confidence",
			want: "must be string, got number",
		},
		"array for object": {
			body: `{"root_cause": [],
			        "remediation_plan": {"steps": [{"action": "a", "risk": "low"}]}}`,
			path: "root_cause",
			want: "must be object, got array",
		},
		"object for array": {
			body: `{"root_cause": {"summary": "s", "confidence": "high"},
			        "remediation_plan": {"steps": {}}}`,
			path: "remediation_plan.steps",
			want: "must be array, got object",
		},
		"string for array": {
			body: `{"root_cause": {"summary": "s", "confidence": "high", "evidence": "x"},
			        "remediation_plan": {"steps": [{"action": "a", "risk": "low"}]}}`,
			path: "root_cause.evidence",
			want: "must be array, got string",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := DecodeAgenticRequest([]byte(tc.body))
			var verrs ValidationErrors
			require.ErrorAs(t, err, &verrs, "a type mismatch is a schema violation, not a syntax error")
			require.Equal(t, map[string]any{tc.path: tc.want}, verrs.Details())
		})
	}
}

// Postgres jsonb refuses \u0000, so a NUL that passes validation becomes a 500
// at the write instead of a 422 at the door.
func TestValidateRejectsNULCharacter(t *testing.T) {
	req := AgenticRequest{
		RootCause: RootCause{
			Summary:    "disk full\x00",
			Scope:      "srv-\x00victoria1",
			Evidence:   []string{"df -h\x00"},
			Confidence: ConfidenceHigh,
		},
		RemediationPlan: RemediationPlan{
			Steps:    []Step{{Action: "rotate\x00", Command: "logrotate -f\x00", Risk: RiskLow}},
			Rollback: []Step{{Action: "undo\x00", Risk: RiskLow}},
		},
		Source: "cli\x00",
	}
	details := req.Validate().Details()
	const want = "must not contain the NUL character"
	for _, path := range []string{
		"root_cause.summary",
		"root_cause.scope",
		"root_cause.evidence[0]",
		"remediation_plan.steps[0].action",
		"remediation_plan.steps[0].command",
		"remediation_plan.rollback[0].action",
		"source",
	} {
		require.Equal(t, want, details[path], "path %s", path)
	}
}

func TestValidateAcceptsCleanStrings(t *testing.T) {
	req := AgenticRequest{
		RootCause:       RootCause{Summary: "s", Confidence: ConfidenceHigh, Evidence: []string{"e"}},
		RemediationPlan: RemediationPlan{Steps: []Step{{Action: "a", Command: "c", Risk: RiskLow}}},
		Source:          "alert-rca",
	}
	require.Empty(t, req.Validate())
}

func TestDecodeAgenticRequestAcceptsVerdictFields(t *testing.T) {
	body := `{
      "root_cause": {
        "summary": "a fleet rollout lacked runAsUser; the corrective re-roll fixed it",
        "detail": "Revision 24 never got an available replica; revision 25 added runAsUser.",
        "caveats": ["rev-24 pod events had expired; causation is inferred from the diff"],
        "confidence": "high"
      },
      "remediation_plan": {
        "status": "self_resolved",
        "steps": [
          {"action": "nothing to do on this deployment", "risk": "low", "when": "now"},
          {"action": "ship securityContext with image bumps", "risk": "medium", "when": "follow_up"}
        ]
      }
    }`
	req, err := DecodeAgenticRequest([]byte(body))
	require.NoError(t, err)
	require.Equal(t, PlanSelfResolved, req.RemediationPlan.Status)
	require.Equal(t, StepNow, req.RemediationPlan.Steps[0].When)
	require.Equal(t, StepFollowUp, req.RemediationPlan.Steps[1].When)
	require.Len(t, req.RootCause.Caveats, 1)
	require.NotEmpty(t, req.RootCause.Detail)
}

func TestValidateAcceptsEveryPlanStatus(t *testing.T) {
	for _, status := range []string{"", PlanActionRequired, PlanSelfResolved, PlanMonitoring, PlanResolved} {
		req := AgenticRequest{
			RootCause:       RootCause{Summary: "s", Confidence: ConfidenceHigh},
			RemediationPlan: RemediationPlan{Status: status, Steps: []Step{{Action: "a", Risk: RiskLow}}},
		}
		require.Empty(t, req.Validate(), "status %q", status)
	}
	require.Equal(t, "resolved", PlanResolved)
}

func TestValidateVerdictFields(t *testing.T) {
	caveats := make([]string, MaxCaveats+1)
	for i := range caveats {
		caveats[i] = "c"
	}
	req := AgenticRequest{
		RootCause: RootCause{
			Summary: "s", Confidence: ConfidenceHigh,
			Detail:  strings.Repeat("d", MaxDetailLen+1),
			Caveats: caveats,
		},
		RemediationPlan: RemediationPlan{
			Status:   "fixed-itself",
			Steps:    []Step{{Action: "a", Risk: RiskLow, When: "later"}},
			Rollback: []Step{{Action: "b", Risk: RiskLow, When: StepNow}},
		},
	}
	details := req.Validate().Details()
	require.Equal(t, "must be at most 2000 characters", details["root_cause.detail"])
	require.Equal(t, "must hold at most 5 items", details["root_cause.caveats"])
	require.Equal(t, "must be one of action_required|self_resolved|monitoring|resolved", details["remediation_plan.status"])
	require.Equal(t, "must be one of now|follow_up", details["remediation_plan.steps[0].when"])
	require.NotContains(t, details, "remediation_plan.rollback[0].when")

	req = AgenticRequest{
		RootCause:       RootCause{Summary: "s", Confidence: ConfidenceHigh, Caveats: []string{" ", strings.Repeat("c", MaxCaveatLen+1)}},
		RemediationPlan: RemediationPlan{Steps: []Step{{Action: "a", Risk: RiskLow}}},
	}
	details = req.Validate().Details()
	require.Equal(t, "must not be empty", details["root_cause.caveats[0]"])
	require.Equal(t, "must be at most 300 characters", details["root_cause.caveats[1]"])
}
