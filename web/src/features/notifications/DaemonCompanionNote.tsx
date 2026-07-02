import type { Metadata } from "@/shared/forms/types";
import { Icon } from "@/shared/icons/Icon";
import styles from "./DaemonCompanionNote.module.css";

type Daemon = NonNullable<Metadata["daemon"]>;

/**
 * DaemonCompanionNote — a non-blocking banner shown inside the action's
 * configure step when the integration has an optional companion daemon. It
 * replaces the old either/or "chooser" step, which framed a hard dependency as
 * a free choice: an operator could pick "Advanced", set up the daemon, and walk
 * away without ever creating an Action, so nothing was delivered. The built-in
 * action is mandatory and always configured; the daemon is surfaced here as a
 * clearly-secondary add-on.
 */
export function DaemonCompanionNote({ daemon }: { daemon: Daemon }) {
  return (
    <aside className={styles.note} role="note">
      <span className={styles.icon} aria-hidden="true">
        <Icon name="info" size={16} />
      </span>
      <div>
        <p className={styles.title}>Optional companion — {daemon.name}</p>
        <p className={styles.body}>
          {daemon.blurb} That is handled by the separate {daemon.name} daemon and is{" "}
          <strong>not required</strong> — this action delivers on its own once saved.{" "}
          <a
            className={styles.link}
            href={daemon.doc_url}
            target="_blank"
            rel="noreferrer noopener"
          >
            Set up {daemon.name} ↗
          </a>
        </p>
      </div>
    </aside>
  );
}
