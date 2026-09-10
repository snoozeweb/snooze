import { describe, expect, it } from "vitest";
import { groupDeliveries } from "./group";
import { deliveryGroupAriaLabel } from "./format";
import type { DeliveryEntry } from "./types";

/** One send by `action`, part of the dispatch queued at `queued_epoch`. */
function send(action: string, over: Partial<DeliveryEntry> = {}): DeliveryEntry {
  return {
    uid: `d-${action}-${over.queued_epoch ?? 100}`,
    date_epoch: 1757340000,
    queued_epoch: 100,
    status: "success",
    action,
    notifier: "mail",
    notification_uids: ["n1"],
    notification_names: ["notif-a"],
    alert_count: 1,
    alert_uids: ["a1"],
    alert_hashes: ["h1"],
    alerts: [{ uid: "a1", hash: "h1", host: "db-01", severity: "critical", message: "disk 98%" }],
    ...over,
  };
}

describe("groupDeliveries", () => {
  it("folds one dispatch's actions into a single group", () => {
    const groups = groupDeliveries([
      send("mail-oncall"),
      send("slack-noc"),
      send("jira-ops", { status: "error", error: "401 unauthorized" }),
    ]);

    expect(groups).toHaveLength(1);
    const [g] = groups;
    // Chips are ordered failures-first, then by name — never by the race
    // between concurrent sends that the server's order reflects.
    expect(g?.rows.map((r) => r.action)).toEqual(["jira-ops", "mail-oncall", "slack-noc"]);
    expect(g?.failed).toBe(1);
    expect(g?.notifications).toEqual(["notif-a"]);
    expect(g?.alertCount).toBe(1);
    expect(deliveryGroupAriaLabel(g!)).toMatch(
      /^Sent via mail-oncall, slack-noc — failed via jira-ops, 1 alert, /,
    );
  });

  it("prints the newest completion in the group and keeps the list order", () => {
    const groups = groupDeliveries([
      send("mail-oncall", { date_epoch: 1757340009 }),
      send("slack-noc", { date_epoch: 1757340002 }),
      send("late", { queued_epoch: 90, date_epoch: 1757330000 }),
    ]);

    expect(groups.map((g) => g.dateEpoch)).toEqual([1757340009, 1757330000]);
  });

  it("never merges two notifications that fired for the same alert", () => {
    const groups = groupDeliveries([
      send("mail-oncall"),
      send("mail-oncall", {
        uid: "d-other",
        notification_uids: ["n2"],
        notification_names: ["notif-b"],
      }),
    ]);

    expect(groups).toHaveLength(2);
    expect(groups.map((g) => g.notifications)).toEqual([["notif-a"], ["notif-b"]]);
  });

  it("never merges a re-escalation into the delivery it escalated from", () => {
    const groups = groupDeliveries([
      send("mail-oncall", { escalation_count: 2, escalation_reason: "timeout" }),
      send("mail-oncall", { uid: "d-first" }),
    ]);

    expect(groups).toHaveLength(2);
    expect(groups[0]?.escalation).toBe("Re-escalated x2 (timeout)");
    expect(groups[1]?.escalation).toBeNull();
  });

  it("never merges a batched flush with an unbatched send from the same second", () => {
    const groups = groupDeliveries([
      send("webhook-ops", {
        batch: true,
        batch_reason: "size",
        alert_count: 2,
        alert_uids: ["a1", "a2"],
        alert_hashes: ["h1", "h2"],
      }),
      send("mail-oncall"),
    ]);

    expect(groups).toHaveLength(2);
    expect(groups[0]?.batch).toBe("Batch · 2 alerts");
    expect(groups[1]?.batch).toBeNull();
  });

  it("falls back to the completion time when the row predates queued_epoch", () => {
    const unqueued = (action: string, dateEpoch: number): DeliveryEntry => {
      const row = send(action, { date_epoch: dateEpoch });
      delete row.queued_epoch;
      return row;
    };
    const rows = [
      unqueued("mail-oncall", 1757340000),
      unqueued("slack-noc", 1757340000),
      unqueued("pager", 1757339000),
    ];

    const groups = groupDeliveries(rows);
    expect(groups).toHaveLength(2);
    expect(groups[0]?.rows).toHaveLength(2);
  });

  it("returns nothing for an empty page", () => {
    expect(groupDeliveries([])).toEqual([]);
  });
});
