import { afterEach, describe, expect, it } from "vitest";
import { setSeverityRanks } from "@/lib/format/severity-color";
import {
  batchLabel,
  deliveryAriaLabel,
  deliveryStatusLabel,
  escalationBadge,
  sortAlertsBySeverity,
} from "./format";
import type { DeliveryEntry } from "./types";

describe("deliveryStatusLabel", () => {
  it("speaks operator words, not HTTP words", () => {
    expect(deliveryStatusLabel("success")).toBe("Sent");
    expect(deliveryStatusLabel("error")).toBe("Failed");
    expect(deliveryStatusLabel(undefined)).toBe("Sent");
  });
});

describe("batchLabel", () => {
  it("is null for an unbatched row", () => {
    expect(batchLabel({ batch: false, alert_count: 1 })).toBeNull();
    expect(batchLabel({})).toBeNull();
  });
  it("counts the members and pluralises", () => {
    expect(batchLabel({ batch: true, alert_count: 7 })).toBe("Batch · 7 alerts");
    expect(batchLabel({ batch: true, alert_count: 1 })).toBe("Batch · 1 alert");
  });
  it("falls back to the snapshot length", () => {
    expect(batchLabel({ batch: true, alerts: [{ hash: "h1" }, { hash: "h2" }] })).toBe(
      "Batch · 2 alerts",
    );
  });
});

describe("escalationBadge", () => {
  it("is null for a first delivery", () => {
    expect(escalationBadge({ escalation_count: 0 })).toBeNull();
  });
  it("reuses the alerts-page vocabulary", () => {
    expect(escalationBadge({ escalation_count: 2, escalation_reason: "timeout" })).toBe(
      "Re-escalated x2 (timeout)",
    );
  });
});

describe("sortAlertsBySeverity", () => {
  afterEach(() => setSeverityRanks(null));

  it("orders worst first and puts unknown labels last", () => {
    const sorted = sortAlertsBySeverity([
      { host: "d", severity: "info" },
      { host: "z", severity: "banana" },
      { host: "b", severity: "err" },
      { host: "a", severity: "crit" },
      { host: "c", severity: "warning" },
    ]);
    expect(sorted.map((a) => a.host)).toEqual(["a", "b", "c", "d", "z"]);
  });

  it("honours a custom server rank rather than a static alias table", () => {
    // The badge colour beside the line resolves through the server ladder
    // (severityColor → rankOf). Ranking on a second, static map here would
    // print a red badge below an orange one the moment an operator inserts a
    // custom severity — which is exactly what this ordering is meant to avoid.
    setSeverityRanks({ pagerduty: 1, chatter: 7 });
    const sorted = sortAlertsBySeverity([
      { host: "info", severity: "info" },
      { host: "chatter", severity: "chatter" },
      { host: "critical", severity: "crit" },
      { host: "pagerduty", severity: "pagerduty" },
    ]);
    expect(sorted.map((a) => a.host)).toEqual(["pagerduty", "critical", "info", "chatter"]);
  });

  it("is stable within one severity and does not mutate the input", () => {
    const input = [
      { host: "first", severity: "critical" },
      { host: "second", severity: "critical" },
    ];
    const sorted = sortAlertsBySeverity(input);
    expect(sorted.map((a) => a.host)).toEqual(["first", "second"]);
    expect(input[0]!.host).toBe("first");
    expect(sorted).not.toBe(input);
  });
});

describe("deliveryAriaLabel", () => {
  it("summarises the row as one sentence", () => {
    const row: DeliveryEntry = {
      status: "success",
      action: "mail-oncall",
      alert_count: 2,
      date_epoch: Math.floor(Date.now() / 1000),
    };
    const label = deliveryAriaLabel(row);
    expect(label).toMatch(/^Sent via mail-oncall, 2 alerts, /);
  });

  it("says Failed and drops the alert count when there is none", () => {
    expect(deliveryAriaLabel({ status: "error", action: "page", alert_count: 0 })).toBe(
      "Failed via page",
    );
  });
});
