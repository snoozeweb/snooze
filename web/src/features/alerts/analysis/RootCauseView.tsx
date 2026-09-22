// Why the alert fired.
//
// The summary is the lead sentence and the largest type in the pane: it is the
// one line an operator reads before deciding whether to act, and everything
// else here (what is broken, what the conclusion rests on, how much to trust
// it) is support for it.
import { Badge } from "@/shared/ui/Badge";
import type { components } from "@/lib/api/types.gen";
import { ConfidenceBadge } from "./ConfidenceBadge";
import { isConfidence } from "./enums";
import { EvidenceList } from "./EvidenceList";
import styles from "./RootCauseView.module.css";

export type AgenticRootCause = components["schemas"]["AgenticRootCause"];

export type RootCauseViewProps = {
  rootCause: AgenticRootCause;
  /**
   * Show the confidence chip beside the scope.
   *
   * AnalysisTab turns it off: that surface already carries the chip in its
   * header strip, beside the provenance, and two identical chips a line apart
   * say the same thing twice. Any surface that renders a root cause on its own
   * keeps the default — the confidence is a property of the conclusion, not of
   * the inspector that happens to show it.
   */
  showConfidence?: boolean;
};

export function RootCauseView({ rootCause, showConfidence = true }: RootCauseViewProps) {
  const scope = rootCause.scope ?? "";
  const evidence = rootCause.evidence ?? [];
  // `confidence` is a closed enum on the wire but loose JSON in the store: a
  // subtree written by an older server, or hand-edited in the DB, can carry
  // anything. Passing it straight through gave Badge `variant={undefined}` —
  // an unlabelled chip claiming a judgement nobody made. Dropping the chip
  // matches AnalysedDot, which renders nothing for the same input.
  const confidence = isConfidence(rootCause.confidence) ? rootCause.confidence : undefined;
  const withConfidence = showConfidence && confidence !== undefined;
  return (
    <section className={styles.rootCause}>
      <p className={styles.summary}>{rootCause.summary}</p>
      {scope !== "" || withConfidence ? (
        <div className={styles.meta}>
          {scope !== "" ? (
            <Badge variant="muted" className={styles.scope!} title={`Scope: ${scope}`}>
              {scope}
            </Badge>
          ) : null}
          {withConfidence ? <ConfidenceBadge confidence={confidence} /> : null}
        </div>
      ) : null}
      {evidence.length > 0 ? (
        <>
          <h3 className={styles.heading}>Evidence</h3>
          <EvidenceList items={evidence} />
        </>
      ) : null}
    </section>
  );
}
