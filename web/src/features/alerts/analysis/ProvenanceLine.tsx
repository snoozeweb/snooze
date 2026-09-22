// Who wrote this analysis, with what, and when.
//
// The server stamps `analysis` on every write (clients may not set it), so the
// line is the operator's answer to "can I act on this?" — an analysis from the
// alert-rca loop three days ago reads differently from one a colleague wrote
// ten minutes ago. Every part is optional on the wire; an absent part is
// dropped rather than rendered as a placeholder.
import { TimeCell } from "@/shared/ui/TimeCell";
import type { components } from "@/lib/api/types.gen";
import { analysisEpoch } from "./time";
import styles from "./ProvenanceLine.module.css";

export type AnalysisMeta = components["schemas"]["AgenticAnalysisMeta"];

export type ProvenanceLineProps = {
  analysis: AnalysisMeta | undefined;
  className?: string;
};

export function ProvenanceLine({ analysis, className }: ProvenanceLineProps) {
  const source = analysis?.source ?? "";
  const by = analysis?.by ?? "";
  const epoch = analysisEpoch(analysis?.at);
  // "by alert-rca · agent-bot": the tool first, the authenticated subject
  // second, both mono because both are identifiers rather than prose.
  const who = [source, by].filter((v) => v !== "");
  if (who.length === 0 && epoch === undefined) return null;

  const classes = [styles.line, className].filter(Boolean).join(" ");
  return (
    <p className={classes}>
      {who.length > 0 ? (
        <span>
          {"by "}
          {who.map((v, i) => (
            // Composed with the index: the tool and the subject are the same
            // string whenever a human saves from the web UI under a login
            // named for it, and `key={v}` collided there.
            <span key={`${i}-${v}`}>
              {i > 0 ? " · " : null}
              <span className={styles.who}>{v}</span>
            </span>
          ))}
        </span>
      ) : null}
      {who.length > 0 && epoch !== undefined ? <span aria-hidden="true">{" · "}</span> : null}
      {epoch !== undefined ? <TimeCell epoch={epoch} compact /> : null}
    </p>
  );
}
