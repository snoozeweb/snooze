// The limits of the investigation: what could not be checked, what is
// inferred rather than observed.
//
// Read after the plan (it qualifies what to do) and before the evidence (it
// qualifies what the evidence proves), in a quiet callout so it is noticed
// without being mistaken for an alarm. Caveats come from `readCaveats`, which
// also lifts the "caveat: …" lines older agents filed under evidence — the
// prod analysis's only caveat was evidence #7, set in code font.
import { useId } from "react";
import { Icon } from "@/shared/icons/Icon";
import styles from "./CaveatsCallout.module.css";

export type CaveatsCalloutProps = {
  caveats: readonly string[];
};

export function CaveatsCallout({ caveats }: CaveatsCalloutProps) {
  const headingId = useId();
  if (caveats.length === 0) return null;
  return (
    <section className={styles.callout} aria-labelledby={headingId}>
      <Icon name="info" size={16} className={styles.icon!} />
      <div className={styles.content}>
        <h3 className={styles.heading} id={headingId}>
          Caveats
        </h3>
        <ul className={styles.list}>
          {caveats.map((caveat, i) => (
            <li key={`${i}-${caveat}`} className={styles.item}>
              {caveat}
            </li>
          ))}
        </ul>
      </div>
    </section>
  );
}
