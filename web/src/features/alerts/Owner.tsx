// Who is working on an alert, as the table cell and the row inspector show it.
//
// Both read the untyped ownership keys the comment plugin, bulk_owner and the
// automatic re-open/re-escalation paths write onto the record (owner,
// owner_method, owner_since, previous_owner, previous_owner_method). "Unowned"
// is `owner` absent or `""` — clearing writes an explicit empty string — and a
// previous owner only ever shows while nobody owns the alert.
import { Avatar } from "@/shared/ui/Avatar";
import { TimeCell } from "@/shared/ui/TimeCell";
import { personLabel, usePerson } from "@/shared/people/api";
import { OWNER_NOUN, PREVIOUS_OWNER_NOUN, UNOWNED_NOUN } from "./lifecycle";
import { recordOwnership, sincePhrase } from "./ownership";
import type { Record_ } from "./types";
import styles from "./Owner.module.css";

function OwnerTooltip({ title, detail }: { title: string; detail: string }) {
  return (
    <span className={styles.tooltip}>
      <span className={styles.tooltipTitle}>{title}</span>
      {detail ? <span className={styles.tooltipDetail}>{detail}</span> : null}
    </span>
  );
}

/** The Owner column's cell: a face, a faded face, or an em-dash. */
export function OwnerCell({ record }: { record: Record_ }) {
  const o = recordOwnership(record);
  const ownerPerson = usePerson(o.owner, o.ownerMethod || undefined);
  const previousPerson = usePerson(o.previous, o.previousMethod || undefined);
  if (o.owner !== "") {
    const name = personLabel(ownerPerson, o.owner);
    const since = sincePhrase(o.since);
    return (
      <span className={styles.cell}>
        <Avatar
          name={o.owner}
          method={o.ownerMethod || undefined}
          label={`${OWNER_NOUN}: ${name}${since ? `, ${since}` : ""}`}
          tooltip={<OwnerTooltip title={name} detail={since ? `${OWNER_NOUN} ${since}` : ""} />}
        />
      </span>
    );
  }
  if (o.previous !== "") {
    const name = personLabel(previousPerson, o.previous);
    return (
      <span className={styles.cell}>
        <Avatar
          name={o.previous}
          method={o.previousMethod || undefined}
          variant="ghost"
          label={`${PREVIOUS_OWNER_NOUN}: ${name} — unowned now`}
          tooltip={<OwnerTooltip title={name} detail={`${PREVIOUS_OWNER_NOUN} · unowned now`} />}
        />
      </span>
    );
  }
  return <span className={styles.none}>—</span>;
}

/**
 * The row inspector's ownership line, beside the state badges: the owner with
 * their name and since-when, or the previous owner as a ghost. Renders
 * nothing for an alert nobody has ever owned — the Unowned state is the
 * default and earns no chrome.
 */
export function OwnerSummary({ record }: { record: Record_ }) {
  const o = recordOwnership(record);
  const ownerPerson = usePerson(o.owner, o.ownerMethod || undefined);
  const previousPerson = usePerson(o.previous, o.previousMethod || undefined);
  if (o.owner !== "") {
    return (
      <div className={styles.summary} data-slot="owner">
        <Avatar name={o.owner} method={o.ownerMethod || undefined} size="sm" decorative />
        <span className={styles.summaryText}>
          <span className={styles.summaryLabel}>{OWNER_NOUN}</span>{" "}
          <span className={styles.summaryName}>{personLabel(ownerPerson, o.owner)}</span>
          {o.since ? (
            <>
              {" · since "}
              <TimeCell epoch={o.since} compact />
            </>
          ) : null}
        </span>
      </div>
    );
  }
  if (o.previous !== "") {
    return (
      <div className={styles.summary} data-slot="previous-owner">
        <Avatar
          name={o.previous}
          method={o.previousMethod || undefined}
          size="sm"
          variant="ghost"
          decorative
        />
        <span className={styles.summaryText}>
          <span className={styles.summaryLabel}>{UNOWNED_NOUN}</span>
          {" · previously "}
          <span className={styles.summaryName}>{personLabel(previousPerson, o.previous)}</span>
        </span>
      </div>
    );
  }
  return null;
}
