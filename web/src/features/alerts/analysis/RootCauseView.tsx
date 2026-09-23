// The verdict: what the alert needs, and why it fired, in one line.
//
// This is what an on-call engineer reads before anything else in the pane, so
// it is built as a headline and a body rather than as one paragraph. The
// headline is `summary` — or, for an analysis that ignored the "one sentence"
// contract (the prod analysis carried a 430-character summary and no detail),
// its first sentence, recovered by `splitSummary` so display type is never set
// on seven lines of prose. Everything the agent wrote survives in the body.
//
// Evidence and caveats are NOT here: they are support for the verdict, read
// after the plan, and AnalysisTab lays them out in their own sections.
import type { components } from "@/lib/api/types.gen";
import { ConfidenceMeter } from "./ConfidenceMeter";
import { isConfidence } from "./enums";
import { VerdictChip } from "./VerdictChip";
import { splitSummary, type PlanStatus } from "./verdict";
import styles from "./RootCauseView.module.css";

export type AgenticRootCause = components["schemas"]["AgenticRootCause"];

export type RootCauseViewProps = {
  rootCause: AgenticRootCause;
  /**
   * The plan's verdict (`remediation_plan.status`), already narrowed. It
   * belongs to the plan on the wire but leads the verdict on screen: "act /
   * watch / recovered" is the first thing the reader needs.
   */
  status?: PlanStatus | undefined;
  /**
   * Show the confidence meter beside the scope.
   *
   * AnalysisTab turns it off: that surface carries the meter in its provenance
   * row, and two identical meters a line apart say the same thing twice. Any
   * surface that renders a root cause on its own keeps the default — the
   * confidence is a property of the conclusion, not of the inspector that
   * happens to show it.
   */
  showConfidence?: boolean;
};

export function RootCauseView({ rootCause, status, showConfidence = true }: RootCauseViewProps) {
  const scope = rootCause.scope ?? "";
  // `summary`/`detail` are typed strings, but the subtree is loose JSON in the
  // store: anything else reads as absent rather than throwing in trim().
  const summary = typeof rootCause.summary === "string" ? rootCause.summary : "";
  const detail = typeof rootCause.detail === "string" ? rootCause.detail : undefined;
  const { headline, body } = splitSummary(summary, detail);
  // `confidence` is a closed enum on the wire but loose JSON in the store: a
  // subtree written by an older server, or hand-edited in the DB, can carry
  // anything. Dropping the meter matches AnalysedDot, which renders nothing
  // for the same input.
  const confidence = isConfidence(rootCause.confidence) ? rootCause.confidence : undefined;
  const withConfidence = showConfidence && confidence !== undefined;
  return (
    <section className={styles.verdict}>
      {status !== undefined ? <VerdictChip status={status} /> : null}
      {/* A real heading: it is the pane's title in every sense but the
          drawer's, and it is what a screen-reader user jumps to first. */}
      <h3 className={styles.headline}>{headline}</h3>
      {body !== "" ? (
        <p className={styles.body} data-testid="verdict-body">
          {body}
        </p>
      ) : null}
      {scope !== "" || withConfidence ? (
        <div className={styles.meta}>
          {scope !== "" ? (
            <span className={styles.scope} title={`Scope: ${scope}`}>
              {scope}
            </span>
          ) : null}
          {withConfidence ? <ConfidenceMeter confidence={confidence} /> : null}
        </div>
      ) : null}
    </section>
  );
}
