import { describe, expect, it } from "vitest";
import { ALERT_TABS, tabById } from "./tabs";

describe("alert tabs catalog", () => {
  it("exposes the seven canonical tabs in display order", () => {
    expect(ALERT_TABS.map((t) => t.id)).toEqual([
      "alerts",
      "snoozed",
      "ack",
      "esc",
      "closed",
      "shelved",
      "all",
    ]);
  });

  it("default Alerts tab excludes ack, close, snoozed, and shelved records", () => {
    const tab = tabById("alerts");
    expect(tab.condition).toEqual({
      type: "AND",
      args: [
        { type: "NOT", arg: { type: "EQUALS", field: "state", value: "ack" } },
        { type: "NOT", arg: { type: "EQUALS", field: "state", value: "close" } },
        { type: "NOT", arg: { type: "EXISTS", field: "snoozed" } },
        // Shelving flips ttl negative (useShelveRecord/computeNextTTL); this
        // clause is what actually drops a shelved row out of the default view.
        { type: "NOT", arg: { type: "LT", field: "ttl", value: 0 } },
      ],
    });
  });

  it("shelving (ttl < 0) excludes an alert from the default Alerts tab", () => {
    const tab = tabById("alerts");
    const args = tab.condition?.type === "AND" ? tab.condition.args : [];
    expect(args).toContainEqual({ type: "NOT", arg: { type: "LT", field: "ttl", value: 0 } });
  });

  it("Snoozed tab matches records with a snoozed field set", () => {
    expect(tabById("snoozed").condition).toEqual({ type: "EXISTS", field: "snoozed" });
  });

  it("Re-escalated tab is an OR of state=esc and state=open", () => {
    expect(tabById("esc").condition).toEqual({
      type: "OR",
      args: [
        { type: "EQUALS", field: "state", value: "esc" },
        { type: "EQUALS", field: "state", value: "open" },
      ],
    });
  });

  it("Shelved tab matches state==shelved (new-model) OR legacy ttl predicates", () => {
    expect(tabById("shelved").condition).toEqual({
      type: "OR",
      args: [
        { type: "EQUALS", field: "state", value: "shelved" },
        { type: "NOT", arg: { type: "EXISTS", field: "ttl" } },
        { type: "LT", field: "ttl", value: 0 },
      ],
    });
  });

  it("All tab applies no condition", () => {
    expect(tabById("all").condition).toBeNull();
  });

  it("tabById falls back to the default Alerts tab on unknown ids", () => {
    expect(tabById("does-not-exist").id).toBe("alerts");
    expect(tabById(undefined).id).toBe("alerts");
  });
});
