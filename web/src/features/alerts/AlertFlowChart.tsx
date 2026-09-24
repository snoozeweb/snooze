// AlertFlowChart — the pipeline path the alert's last occurrence took:
//   input → rules → aggregate → snooze → notifications → actions
// The stage list is fixed: every stage renders even when it matched nothing,
// because "which stage let this through?" is only answerable if the stages that
// stayed quiet are visible too. The record's plugin trail says where the run
// stopped — at the snooze (silenced) or at the aggregate (a throttled repeat) —
// and everything after the stop is greyed out on a dashed rail, with what the
// last run that did notify sent shown as history rather than as this run.
// Drawn as a stepper: a rail of markers down the drawer's Flow tab, turning
// horizontal when the drawer is wide enough. All data comes from the record row; no
// fetch. Every entity (rule, aggregate, snooze, notification, action)
// deep-links to its management page with the page's search filter pre-set to
// the clicked object by name (?search=name = "X").
import { Fragment, type ReactNode } from "react";
import { Link } from "@tanstack/react-router";
import { Badge, type BadgeVariant } from "@/shared/ui/Badge";
import { Tooltip } from "@/shared/ui/Tooltip";
import type { Record_ } from "./types";
import { nameQuery } from "./nameQuery";
import styles from "./AlertFlowChart.module.css";

type ActionResult = NonNullable<Record_["actions"]>[number];

const ACTION_VARIANT: Record<string, BadgeVariant> = {
  success: "ok",
  error: "error",
  skipped: "muted",
  pending: "warning",
  sent: "neutral",
};

const ACTION_GLYPH: Record<string, string> = {
  success: "✓",
  error: "✗",
  skipped: "⊘",
  pending: "⏳",
  sent: "➤",
};

const ACTION_HINT: Record<string, string> = {
  skipped: "Skipped — notification frequency is off",
  pending: "Send in progress",
  sent: "Dispatched — outcome tracking disabled",
};

function ActionChip({ action }: { action: ActionResult }) {
  const status = action.status ?? "sent";
  const variant = ACTION_VARIANT[status] ?? "neutral";
  const glyph = ACTION_GLYPH[status] ?? "";
  const badge = (
    <Badge variant={variant} className={styles.actionBadge ?? ""}>
      <span aria-hidden="true">{glyph}</span>
      {action.name}
    </Badge>
  );
  // The error message and the sent/skipped hints are both surfaced as a hover
  // tooltip, which frees the chip's click to deep-link to the Actions page.
  const tip = status === "error" && action.error ? action.error : ACTION_HINT[status];

  // An action with no name can't be deep-linked — render the badge as plain
  // text (with its tooltip, if any) so nothing is dropped.
  if (!action.name) {
    const plain = <span className={styles.chip}>{badge}</span>;
    return tip ? <Tooltip content={tip}>{plain}</Tooltip> : plain;
  }

  const link = (
    <Link
      to="/web/notifications"
      search={{ tab: "actions", actionSearch: nameQuery(action.name) }}
      className={[styles.chip, styles.chipLink].join(" ")}
    >
      {badge}
    </Link>
  );
  return tip ? <Tooltip content={tip}>{link}</Tooltip> : link;
}

// notificationBranches groups the alert's actions under the notification that
// triggered each (action.notification). Branch order follows row.notifications;
// any notification referenced only by an action is appended. A matched
// notification with no actions still gets a branch. Actions with no/unknown
// attribution fall into a trailing headerless bucket so nothing is dropped.
function notificationBranches(
  notifications: string[],
  actions: ActionResult[],
): { name: string; actions: ActionResult[] }[] {
  // De-duplicate notification names. Names are not unique across notification
  // entries (entries are keyed by uid, not name), and a branch's actions are
  // attributed purely by name (see the filter below) — so two entries sharing
  // a name are indistinguishable here and must collapse into one branch.
  // Without this, `["oncall", "oncall"]` would render two identical branches
  // (same key, same filtered actions).
  const order: string[] = [];
  for (const n of notifications) {
    if (!order.includes(n)) order.push(n);
  }
  for (const a of actions) {
    const n = a.notification ?? "";
    if (n && !order.includes(n)) order.push(n);
  }
  const branches = order.map((name) => ({
    name,
    actions: actions.filter((a) => (a.notification ?? "") === name),
  }));
  const orphaned = actions.filter((a) => !(a.notification ?? ""));
  if (orphaned.length > 0) {
    branches.push({ name: "", actions: orphaned });
  }
  return branches;
}

function NotificationBranch({ name, actions }: { name: string; actions: ActionResult[] }) {
  return (
    <div className={styles.branch}>
      {name ? (
        <Link
          to="/web/notifications"
          search={{ tab: "notifications", search: nameQuery(name) }}
          className={styles.branchHead}
        >
          {name}
        </Link>
      ) : null}
      {name ? (
        <span className={styles.branchArrow} aria-hidden="true">
          →
        </span>
      ) : null}
      {actions.length > 0 ? (
        actions.map((a, i) => <ActionChip key={`${a.name ?? "action"}-${i}`} action={a} />)
      ) : (
        <span className={styles.none}>no actions</span>
      )}
    </div>
  );
}

/**
 * How a stage fared on the alert's last run:
 *   passed   — the run went through it (whether or not anything matched)
 *   held     — the run stopped here, before notification
 *   skipped  — never reached: greyed out, reached by a dashed rail
 *   notified — the notification stage ran and sent
 *   failed   — the notification stage ran and an action failed
 */
type StageStatus = "passed" | "held" | "skipped" | "notified" | "failed";

const STATUS_TEXT: Partial<Record<StageStatus, string>> = {
  held: "stopped here",
  skipped: "not reached",
  failed: "an action failed",
};

function Stage({
  label,
  status,
  children,
}: {
  label: string;
  status: StageStatus;
  children: ReactNode;
}) {
  const said = STATUS_TEXT[status];
  return (
    <li className={styles.stage} data-status={status}>
      <span className={styles.marker} aria-hidden="true" />
      <span className={styles.label}>
        {label}
        {said ? <span className={styles.srOnly}>{` (${said})`}</span> : null}
      </span>
      <div className={styles.body}>{children}</div>
    </li>
  );
}

export function AlertFlowChart({ row }: { row: Record_ }) {
  const rules = row.rules ?? [];
  const notifications = row.notifications ?? [];
  const actions = row.actions ?? [];
  const snoozed = row.snoozed;
  const trail = row.plugins ?? [];
  // The plugin trail of the last run says how far it got. `snoozed` names the
  // filter that silenced the alert, but not necessarily THIS run: the snooze
  // plugin keeps it on the recovery (close) of a silenced alert and lets that
  // close through to notification. A row with no trail (older data) reads as
  // silenced when snoozed and as notified otherwise, the only stories that
  // existed before the trail.
  const reachedNotification = trail.length === 0 || trail.includes("notification");
  const passedThrough = !!snoozed && trail.includes("notification");
  const silenced = !!snoozed && !passedThrough;
  // A run that went through the aggregate but not to notification, with no
  // snooze to blame, was held by the aggregate: a repeat inside the throttle
  // window (or a flapping alert, or a repeat of a closed one). The snooze
  // stage still ran — as a filter, to keep the silence attribution honest —
  // but nothing was sent. Without this the chart drew such a run as notified,
  // with the notifications of whichever earlier run did reach them.
  const held = !silenced && !reachedNotification && trail.includes("aggregaterule");
  const stopped = silenced || held;
  // Notifications and actions persist from the last run that reached them; on
  // a run that stopped short they are history, shown as such.
  const branches = notificationBranches(notifications, actions);
  const anyFailed = actions.some((a) => a.status === "error");
  const notificationStatus: StageStatus = stopped
    ? "skipped"
    : branches.length === 0
      ? "passed"
      : anyFailed
        ? "failed"
        : "notified";
  // "default" is the aggregaterule plugin's synthetic fallback bucket — it has
  // no backing rule, so a deep-link would dead-end on an empty list. Render it
  // (and the empty "—") as plain text; link only real aggregate rule names.
  const aggregateLinkable = row.aggregate && row.aggregate !== "default";
  const heldReason =
    row.state === "close"
      ? "A repeat of a closed alert — not notified again."
      : "A repeat inside the throttle window — not notified this time.";

  return (
    // The sizing container the stage layout keys off: the drawer's usual
    // width runs the stages down a vertical rail; a wide drawer (a NOC screen)
    // lays the same stages left to right.
    <div className={styles.flowContainer}>
      <ol className={styles.flow} aria-label="Pipeline path of the last occurrence">
        <Stage label="Input" status="passed">
          <span className={styles.value}>{row.source || "—"}</span>
        </Stage>
        <Stage label="Rules" status="passed">
          {rules.length > 0 ? (
            <span className={styles.value}>
              {rules.map((r, i) => (
                <Fragment key={`${r}-${i}`}>
                  {i > 0 ? ", " : null}
                  <Link
                    to="/web/rules"
                    search={{ tab: "rules", search: nameQuery(r) }}
                    className={styles.entityLink}
                  >
                    {r}
                  </Link>
                </Fragment>
              ))}
            </span>
          ) : (
            <span className={styles.none}>none</span>
          )}
        </Stage>
        <Stage label="Aggregate" status={held ? "held" : "passed"}>
          <span className={styles.value}>
            {aggregateLinkable ? (
              <Link
                to="/web/rules"
                search={{ tab: "aggregates", aggSearch: nameQuery(row.aggregate as string) }}
                className={styles.entityLink}
              >
                {row.aggregate}
              </Link>
            ) : (
              row.aggregate || "—"
            )}
          </span>
          {/* hash is an extra key stamped by the aggregaterule plugin; not in
              the Record schema, hence the typeof guard. Truncated (the full
              hash would wrap the stage); the title carries the whole value. */}
          {typeof row.hash === "string" && row.hash ? (
            <span className={styles.subtle} title={row.hash}>
              <span className={styles.subtleLabel}>group key </span>
              {row.hash.slice(0, 12)}
            </span>
          ) : null}
          {held ? <p className={styles.reason}>{heldReason}</p> : null}
        </Stage>
        {/* The snooze stage renders whether or not it fired. An operator woken
            at 03:00 is asking "should this have reached me?", and the silence
            of a stage that matched nothing is an answer — it is just an answer
            nobody can read if the stage is missing from the chart. */}
        <Stage label="Snooze" status={silenced ? "held" : "passed"}>
          {snoozed ? (
            <>
              <Link
                to="/web/snoozes"
                search={{ search: nameQuery(snoozed) }}
                className={styles.chipLink}
              >
                <Badge variant="muted" className={styles.actionBadge ?? ""}>
                  <span aria-hidden="true">⊘</span>
                  {snoozed}
                </Badge>
              </Link>
              <p className={styles.reason}>
                {passedThrough
                  ? "Silenced earlier — this recovery passed through."
                  : "Silenced — the run stopped here."}
              </p>
            </>
          ) : (
            <span className={styles.none}>no snooze matched</span>
          )}
        </Stage>
        <Stage label="Notifications" status={notificationStatus}>
          {stopped ? (
            <>
              <span className={styles.none}>
                {silenced ? "not reached — silenced" : "not reached — held as a repeat"}
              </span>
              {/* What the last run that did get here sent — the answer to
                  "so who has been told about this?". */}
              {branches.length > 0 ? (
                <div className={styles.earlier}>
                  <span className={styles.earlierLabel}>Last notified via</span>
                  <div className={styles.fork}>
                    {branches.map((b, i) => (
                      <NotificationBranch
                        key={`${b.name || "orphaned"}-${i}`}
                        name={b.name}
                        actions={b.actions}
                      />
                    ))}
                  </div>
                </div>
              ) : null}
            </>
          ) : branches.length > 0 ? (
            <div className={styles.fork}>
              {branches.map((b, i) => (
                <NotificationBranch
                  key={`${b.name || "orphaned"}-${i}`}
                  name={b.name}
                  actions={b.actions}
                />
              ))}
            </div>
          ) : (
            <span className={styles.none}>none matched</span>
          )}
        </Stage>
      </ol>
    </div>
  );
}
