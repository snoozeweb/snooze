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

  it("default Alerts tab excludes ack, close, and snoozed records", () => {
    const tab = tabById("alerts");
    expect(tab.condition).toEqual({
      type: "AND",
      args: [
        { type: "NOT", arg: { type: "EQUALS", field: "state", value: "ack" } },
        { type: "NOT", arg: { type: "EQUALS", field: "state", value: "close" } },
        { type: "NOT", arg: { type: "EXISTS", field: "snoozed" } },
      ],
    });
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

  it("Shelved tab matches NOT EXISTS ttl OR ttl<0 (Python 1.x alert.yaml)", () => {
    expect(tabById("shelved").condition).toEqual({
      type: "OR",
      args: [
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
