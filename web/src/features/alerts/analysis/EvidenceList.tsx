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
// When two or more lines share the same probe ("df -h: /var at 100%", "df -h:
// /tmp at 3%"), the probe is printed once as the label of a small group, at
// the position of its first line, with each result under it in recorded
// order. Repeating the same command on every line made the list read as a
// wall of identical mono prefixes, and the results — the part that differs —
// were the part the eye had to hunt for.
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

/** One rendered entry: a plain line, a line with its probe, or a probe's group. */
type Entry =
  | { kind: "line"; item: string; command?: undefined; rest?: undefined }
  | { kind: "probe"; item: string; command: string; rest: string }
  | { kind: "group"; command: string; results: string[] };

/**
 * Folds lines that share a probe into one group, placed where the probe's
 * first line was. A probe seen once stays an ordinary line.
 */
function toEntries(items: readonly string[]): Entry[] {
  const splits = items.map(splitEvidence);
  const counts = new Map<string, number>();
  for (const split of splits) {
    if (split !== null) counts.set(split.command, (counts.get(split.command) ?? 0) + 1);
  }
  const entries: Entry[] = [];
  const groups = new Map<string, { kind: "group"; command: string; results: string[] }>();
  items.forEach((item, i) => {
    const split = splits[i] ?? null;
    if (split === null) {
      entries.push({ kind: "line", item });
      return;
    }
    if ((counts.get(split.command) ?? 0) < 2) {
      entries.push({ kind: "probe", item, command: split.command, rest: split.rest });
      return;
    }
    const existing = groups.get(split.command);
    if (existing) {
      existing.results.push(split.rest);
      return;
    }
    const group = { kind: "group" as const, command: split.command, results: [split.rest] };
    groups.set(split.command, group);
    entries.push(group);
  });
  return entries;
}

export function EvidenceList({ items }: EvidenceListProps) {
  const baseId = useId();
  if (items.length === 0) return null;
  const entries = toEntries(items);
  return (
    <ol className={styles.list}>
      {entries.map((entry, i) => {
        if (entry.kind === "group") {
          const labelId = `${baseId}-group-${i}`;
          return (
            <li key={`${i}-group-${entry.command}`} className={styles.item}>
              <code className={styles.command} id={labelId}>
                {entry.command}
              </code>
              {/* Labelled by the probe, so a screen reader hears "df -h, list,
                  2 items" rather than two unlabelled results. */}
              <ul className={styles.results} aria-labelledby={labelId}>
                {entry.results.map((rest, k) => (
                  <li key={`${k}-${rest}`} className={styles.result}>
                    {rest}
                  </li>
                ))}
              </ul>
            </li>
          );
        }
        return (
          <li key={`${i}-${entry.item}`} className={styles.item}>
            {entry.kind === "line" ? (
              entry.item
            ) : (
              <>
                <code className={styles.command}>{entry.command}</code>
                {`: ${entry.rest}`}
              </>
            )}
          </li>
        );
      })}
    </ol>
  );
}
