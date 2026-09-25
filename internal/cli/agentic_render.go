package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// agenticWrap is the column prose is wrapped at in the human-readable analysis.
const agenticWrap = 88

// decodeAgentic reads the stored `agentic` subtree into its typed shape. The
// server validated it on write, so a failure here means an unexpected shape —
// the caller falls back to the raw dump rather than hiding data.
func decodeAgentic(raw map[string]any) (snoozetypes.Agentic, bool) {
	var a snoozetypes.Agentic
	if raw == nil {
		return a, false
	}
	b, err := json.Marshal(raw)
	if err != nil || json.Unmarshal(b, &a) != nil {
		return a, false
	}
	return a, true
}

// verdictLabel renders a plan status as the chip the web UI shows:
// "action_required" → "ACTION REQUIRED"; no status → "NO VERDICT".
func verdictLabel(status string) string {
	if status == "" {
		return "NO VERDICT"
	}
	return strings.ToUpper(strings.ReplaceAll(status, "_", " "))
}

// agenticSummaryLine is the one-line digest `record show` prints in place of
// the raw subtree.
func agenticSummaryLine(raw map[string]any, uid string) string {
	a, ok := decodeAgentic(raw)
	if !ok {
		return coerce(raw)
	}
	parts := []string{a.RemediationPlan.Status}
	if parts[0] == "" {
		parts[0] = "no verdict"
	}
	if a.RootCause.Confidence != "" {
		parts = append(parts, "confidence "+a.RootCause.Confidence)
	}
	if a.RootCause.Summary != "" {
		parts = append(parts, oneLine(a.RootCause.Summary))
	}
	return strings.Join(parts, " · ") + "  (see: snooze record agentic get " + uid + ")"
}

// renderAgentic prints an analysis for a human: provenance, verdict, the root
// cause (summary, detail, caveats before evidence — they are read before the
// conclusion is trusted), then the plan grouped the way on-call works it.
func renderAgentic(out io.Writer, a snoozetypes.Agentic) {
	rc, plan, meta := a.RootCause, a.RemediationPlan, a.Analysis

	prov := []string{}
	if meta.At != "" {
		prov = append(prov, meta.At)
	}
	if meta.By != "" {
		prov = append(prov, "by "+meta.By)
	}
	if meta.Source != "" {
		prov = append(prov, "source "+meta.Source)
	}
	if len(prov) > 0 {
		_, _ = fmt.Fprintf(out, "Analysis    %s\n", strings.Join(prov, " · "))
	}
	verdict := "[" + verdictLabel(plan.Status) + "]"
	if rc.Confidence != "" {
		verdict += " · confidence " + rc.Confidence
	}
	if plan.Automatable {
		verdict += " · automatable"
	}
	_, _ = fmt.Fprintf(out, "Verdict     %s\n", verdict)
	if rc.Scope != "" {
		_, _ = fmt.Fprintf(out, "Scope       %s\n", rc.Scope)
	}

	section(out, "Summary")
	paragraph(out, rc.Summary)
	if rc.Detail != "" {
		section(out, "Detail")
		paragraph(out, rc.Detail)
	}
	bullets(out, "Caveats", rc.Caveats)
	bullets(out, "Evidence", rc.Evidence)

	var now, follow, unset []snoozetypes.Step
	for _, s := range plan.Steps {
		switch s.When {
		case snoozetypes.StepNow:
			now = append(now, s)
		case snoozetypes.StepFollowUp:
			follow = append(follow, s)
		default:
			unset = append(unset, s)
		}
	}
	steps(out, "Now", now)
	steps(out, "Follow-up", follow)
	// A plan written on the legacy schema has no `when`: list its steps as
	// they came rather than guessing which half they belong to.
	steps(out, "Steps", unset)
	steps(out, "Rollback", plan.Rollback)
}

func section(out io.Writer, title string) {
	_, _ = fmt.Fprintf(out, "\n%s\n", title)
}

// paragraph prints prose wrapped and indented by two spaces.
func paragraph(out io.Writer, text string) {
	for _, line := range wrapText(text, agenticWrap-2) {
		_, _ = fmt.Fprintf(out, "  %s\n", line)
	}
}

func bullets(out io.Writer, title string, items []string) {
	if len(items) == 0 {
		return
	}
	section(out, title)
	for _, it := range items {
		for i, line := range wrapText(it, agenticWrap-4) {
			lead := "  - "
			if i > 0 {
				lead = "    "
			}
			_, _ = fmt.Fprintf(out, "%s%s\n", lead, line)
		}
	}
}

// steps prints a numbered step list: the action wrapped under its number and
// risk, the command on its own line so it can be copied whole.
func steps(out io.Writer, title string, list []snoozetypes.Step) {
	if len(list) == 0 {
		return
	}
	section(out, title)
	for i, s := range list {
		lead := fmt.Sprintf("  %d. [%s] ", i+1, s.Risk)
		hang := strings.Repeat(" ", len(lead))
		for j, line := range wrapText(s.Action, agenticWrap-len(lead)) {
			if j == 0 {
				_, _ = fmt.Fprintf(out, "%s%s\n", lead, line)
			} else {
				_, _ = fmt.Fprintf(out, "%s%s\n", hang, line)
			}
		}
		if s.Command != "" {
			_, _ = fmt.Fprintf(out, "%s$ %s\n", hang, s.Command)
		}
	}
}

// wrapText greedy-wraps text at width, keeping explicit line breaks. A word
// longer than width gets a line of its own rather than being split.
func wrapText(text string, width int) []string {
	var out []string
	for _, para := range strings.Split(strings.TrimSpace(text), "\n") {
		words := strings.Fields(para)
		if len(words) == 0 {
			out = append(out, "")
			continue
		}
		line := words[0]
		for _, w := range words[1:] {
			if len(line)+1+len(w) > width {
				out = append(out, line)
				line = w
				continue
			}
			line += " " + w
		}
		out = append(out, line)
	}
	return out
}

// oneLine collapses whitespace so a summary fits one output line.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
