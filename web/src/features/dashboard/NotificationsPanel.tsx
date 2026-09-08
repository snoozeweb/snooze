// Dashboard breakdown tab: which notifications actually sent, ranked, with a
// deep link into the notification's Deliveries tab for the same window. A
// ranked list rather than a `BarChart` — the chart wrapper has no click
// handler and a list is keyboard-navigable (every row is a real link).
import { useMemo } from "react";
import { Link } from "@tanstack/react-router";
import { Notifications } from "@/features/notifications/api";
import { useCanReadDeliveries } from "@/features/notifications/deliveries/perms";
import { PanelEmpty, PanelHint } from "./Panel";
import { countersEmpty } from "./empty-copy";
import type { StatsCounters } from "./types";
import styles from "./NotificationsPanel.module.css";

/** Top-slice for the ranked list — mirrors BAR_PANEL_CAP's spirit for the
 *  Actions panel, keeping the list scannable without scrolling. */
const TOP_N = 8;

export type NotificationsPanelProps = {
  /** Send counts keyed by notification name (the stats dimension). */
  byNotification: Record<string, number>;
  /** Current dashboard window, epoch seconds — forwarded to the deep link so
   *  the inspector's Deliveries tab opens pre-filtered to the same range.
   *  Absent when the window couldn't be parsed — the link then omits
   *  `from`/`to` entirely rather than pointing at a bogus 0-epoch window. */
  windowFrom?: number;
  windowTo?: number;
  windowLabel: string;
  counters?: StatsCounters;
};

export function NotificationsPanel({
  byNotification,
  windowFrom,
  windowTo,
  windowLabel,
  counters,
}: NotificationsPanelProps) {
  // The link's whole point is the Deliveries tab pre-filtered to this window.
  // A role that cannot read the delivery log would land on an inspector with
  // no Deliveries tab at all, so the name stays plain text for them — and the
  // name→uid lookup that only exists to build that link is not fetched.
  const canReadDeliveries = useCanReadDeliveries();
  const notifList = Notifications.useList({ limit: 500 }, { enabled: canReadDeliveries });

  // First match wins — names aren't guaranteed unique, and this is a
  // best-effort deep link, not an identity lookup.
  const uidByName = useMemo(() => {
    const map = new Map<string, string>();
    for (const n of notifList.data?.data ?? []) {
      if (n.uid && !map.has(n.name)) map.set(n.name, n.uid);
    }
    return map;
  }, [notifList.data]);

  const ranked = useMemo(
    () => Object.entries(byNotification).sort((a, b) => b[1] - a[1]),
    [byNotification],
  );
  const top = ranked.slice(0, TOP_N);
  const max = top.length > 0 ? top[0]![1] : 0;

  if (ranked.length === 0) {
    return (
      <PanelEmpty
        {...countersEmpty(counters, windowLabel, "notifications sent", { terse: true })}
      />
    );
  }

  return (
    <>
      {ranked.length > TOP_N ? <PanelHint>{`Top ${TOP_N} of ${ranked.length}`}</PanelHint> : null}
      <ol className={styles.list}>
        {top.map(([name, count]) => {
          const uid = canReadDeliveries ? uidByName.get(name) : undefined;
          const width = max > 0 ? `${Math.max((count / max) * 100, 2)}%` : "0%";
          return (
            <li key={name} className={styles.row}>
              <span className={styles.nameCell}>
                {uid ? (
                  <Link
                    className={styles.link}
                    to="/web/notifications"
                    search={{
                      tab: "notifications",
                      details: uid,
                      ...(windowFrom !== undefined && windowTo !== undefined
                        ? { from: windowFrom, to: windowTo }
                        : {}),
                    }}
                  >
                    {name}
                  </Link>
                ) : notifList.isSuccess && canReadDeliveries ? (
                  <>
                    <span className={styles.name}>{name}</span>
                    <span className={styles.hint}>no longer exists</span>
                  </>
                ) : (
                  <span className={styles.name}>{name}</span>
                )}
              </span>
              <span className={styles.track} aria-hidden="true">
                <span className={styles.fill} style={{ width }} />
              </span>
              <span className={styles.count}>{count.toLocaleString()}</span>
            </li>
          );
        })}
      </ol>
    </>
  );
}
