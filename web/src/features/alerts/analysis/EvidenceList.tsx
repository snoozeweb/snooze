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
// Erring towards prose is deliberate: mis-setting a sentence in mono is worse
// than leaving a flagged command plain, because the reader stops trusting the
// distinction.
import styles from "./EvidenceList.module.css";

export type EvidenceListProps = {
  items: readonly string[];
};

const SEPARATOR = ": ";

/** Characters that only appear in a path, a flag, a dotted name or a metric. */
const SHAPE_CHARS = /[/\-._=]/;

/** Longest a command line gets before it is more plausibly a sentence. */
const MAX_COMMAND_TOKENS = 3;

/** Does this prefix read as something the operator could paste into a shell? */
function isCommandPrefix(prefix: string): boolean {
  const tokens = prefix.split(/\s+/);
  if (!/^[a-z0-9]/.test(tokens[0] ?? "")) return false;
  if (SHAPE_CHARS.test(prefix)) return true;
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

export function EvidenceList({ items }: EvidenceListProps) {
  if (items.length === 0) return null;
  return (
    <ol className={styles.list}>
      {items.map((item, i) => {
        const split = splitEvidence(item);
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
