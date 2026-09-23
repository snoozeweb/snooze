// Who wrote this analysis, with what, and when.
//
// The server stamps `analysis` on every write (clients may not set it), so the
// line is the operator's answer to "can I act on this?" — an analysis from the
// alert-rca loop three days ago reads differently from one a colleague wrote
// ten minutes ago. Its first job is the one the old "by snooze-cli · snooze"
// line never did: say whether a MODEL wrote this. An unlabelled machine
// conclusion is read with a human's authority, so anything that did not come
// from the web editor leads with "AI analysis" (see `authorOf` in verdict.ts).
//
// Every other part is optional on the wire; an absent part is dropped rather
// than rendered as a placeholder.
import type { ReactNode } from "react";
import { TimeCell } from "@/shared/ui/TimeCell";
import type { components } from "@/lib/api/types.gen";
import { analysisEpoch } from "./time";
import { authorOf } from "./verdict";
import styles from "./ProvenanceLine.module.css";

export type AnalysisMeta = components["schemas"]["AgenticAnalysisMeta"];

export type ProvenanceLineProps = {
  analysis: AnalysisMeta | undefined;
  className?: string;
};

export function ProvenanceLine({ analysis, className }: ProvenanceLineProps) {
  if (analysis === undefined) return null;
  const author = authorOf(analysis);
  const epoch = analysisEpoch(analysis.at);

  // Built as a list and joined with separators, so a missing part never
  // leaves a doubled or dangling " · ".
  const parts: { key: string; node: ReactNode }[] = [];
  if (author.kind === "agent") {
    parts.push({ key: "kind", node: <span className={styles.kind}>AI analysis</span> });
    // The tool, then the authenticated subject it ran as — both identifiers,
    // so both mono. When they are the same name (a service account called
    // after its tool) it is printed once.
    if (author.tool !== "") {
      parts.push({ key: "tool", node: <span className={styles.who}>{author.tool}</span> });
    }
    if (author.by !== "" && author.by !== author.tool) {
      parts.push({ key: "by", node: <span className={styles.who}>{author.by}</span> });
    }
  } else {
    // A person: their login is the byline. The web UI's own `snooze-web` tag
    // is plumbing, not authorship, and is not printed.
    parts.push({
      key: "kind",
      node:
        author.by !== "" ? (
          <span>
            {"Written by "}
            <span className={styles.who}>{author.by}</span>
          </span>
        ) : (
          <span>Written in Snooze</span>
        ),
    });
  }
  if (epoch !== undefined) {
    parts.push({ key: "at", node: <TimeCell epoch={epoch} compact /> });
  }

  const classes = [styles.line, className].filter(Boolean).join(" ");
  return (
    <p className={classes}>
      {parts.map((part, i) => (
        <span key={part.key} className={styles.part}>
          {i > 0 ? (
            <span className={styles.sep} aria-hidden="true">
              ·
            </span>
          ) : null}
          {part.node}
        </span>
      ))}
    </p>
  );
}
