// The observations a root cause rests on, in the order the analysis recorded
// them.
//
// Agents write evidence in two registers: prose ("the unit restarted twice in
// the window") and probe output ("df -h: /var at 100%"). The second is worth
// setting apart — the operator will re-run it — but only the command deserves
// mono, not the whole line.
//
// The split tests the prefix before the first ": " against three things, in
// order, and every one of them has to pass:
//
//   1. it starts lowercase. A command is never capitalised and a sentence
//      lead-in almost always is, so this is what keeps "Note: …", "See:
//      https://…" and "Well-known issue: …" out of mono. The older rule —
//      "no spaces" — promoted all three.
//   2. it holds a /, -, ., _ or = somewhere (a path, a flag, a dotted name, a
//      metric, an assignment), OR
//   3. it is at most three tokens long — the shape of `kubectl get pods` and
//      of a bare `journalctl`, but not of "the unit restarted twice: …".
//
// A shape-free prefix built on an ordinary English lead-in word ("caveat",
// "note", "root cause") fails too, whatever its length: rule 3 alone set an
// older agent's "caveat: could not reach the host" in code font, as if it were
// a probe to re-run.
//
// Erring towards prose is deliberate: mis-setting a sentence in mono is worse
// than leaving a flagged command plain, because the reader stops trusting the
// distinction.
//
// Every line stays its own numbered entry, so the list counts what the
// "Evidence · N" disclosure promises and the numbers keep the recorded order.
// When EVERY line was read through the same probe — the common shape is an
// agent tagging each line with its source, "kubectl --context ovh: …" — the
// probe is printed once above the list and the entries carry only the
// results. Folding them into one list item instead (the earlier design) showed
// "Evidence · 6" over a single "1.", with the six results run together so a
// wrapped line could not be told from the next result. A probe shared by only
// some lines stays on each of its lines: hoisting it would attribute the other
// lines to it.
import { useId } from "react";
import styles from "./EvidenceList.module.css";

export type EvidenceListProps = {
  items: readonly string[];
};

const SEPARATOR = ": ";

/** Characters that only appear in a path, a flag, a dotted name or a metric. */
const SHAPE_CHARS = /[/\-._=]/;

/** Longest a command line gets before it is more plausibly a sentence. */
const MAX_COMMAND_TOKENS = 3;

/**
 * Lead-in words agents put before a colon in prose. No shell command in the
 * fleet is spelled like one of these, and every one of them has been seen in
 * front of a sentence. Only consulted for a prefix with no path/flag shape —
 * `note-sync -v` is still a command.
 */
const PROSE_WORDS = new Set([
  "also",
  "cause",
  "caveat",
  "caveats",
  "conclusion",
  "context",
  "evidence",
  "finding",
  "findings",
  "however",
  "hypothesis",
  "impact",
  "likely",
  "note",
  "notes",
  "observation",
  "reason",
  "result",
  "see",
  "status",
  "summary",
  "symptom",
  "timeline",
  "update",
  "warning",
]);

/** Does this prefix read as something the operator could paste into a shell? */
function isCommandPrefix(prefix: string): boolean {
  const tokens = prefix.split(/\s+/);
  if (!/^[a-z0-9]/.test(tokens[0] ?? "")) return false;
  if (SHAPE_CHARS.test(prefix)) return true;
  if (tokens.some((t) => PROSE_WORDS.has(t))) return false;
  return tokens.length <= MAX_COMMAND_TOKENS;
}

/**
 * Splits one evidence line into its command prefix and the rest, or returns
 * null when the line is plain prose.
 */
function splitEvidence(item: string): { command: string; rest: string } | null {
  const at = item.indexOf(SEPARATOR);
  if (at <= 0) return null;
  const command = item.slice(0, at);
  if (!isCommandPrefix(command)) return null;
  return { command, rest: item.slice(at + SEPARATOR.length) };
}

/**
 * The probe every line shares, or null. Needs two lines at least — a lone
 * probe line reads better inline.
 */
function sharedProbe(splits: readonly ({ command: string; rest: string } | null)[]): string | null {
  if (splits.length < 2) return null;
  const first = splits[0]?.command;
  if (first === undefined) return null;
  return splits.every((split) => split?.command === first) ? first : null;
}

export function EvidenceList({ items }: EvidenceListProps) {
  const sourceId = useId();
  if (items.length === 0) return null;
  const splits = items.map(splitEvidence);
  const shared = sharedProbe(splits);
  if (shared !== null) {
    return (
      <div className={styles.wrap}>
        <p className={styles.source} id={sourceId}>
          All read with <code className={styles.command}>{shared}</code>
        </p>
        {/* Labelled by the probe, so a screen reader hears which command
            the results came from before it hears them. */}
        <ol className={styles.list} aria-labelledby={sourceId}>
          {splits.map((split, i) => (
            <li key={`${i}-${split?.rest ?? ""}`} className={styles.item}>
              {split?.rest}
            </li>
          ))}
        </ol>
      </div>
    );
  }
  return (
    <ol className={styles.list}>
      {items.map((item, i) => {
        const split = splits[i] ?? null;
        return (
          <li key={`${i}-${item}`} className={styles.item}>
            {split === null ? (
              item
            ) : (
              <>
                <code className={styles.command}>{split.command}</code>
                {`: ${split.rest}`}
              </>
            )}
          </li>
        );
      })}
    </ol>
  );
}
