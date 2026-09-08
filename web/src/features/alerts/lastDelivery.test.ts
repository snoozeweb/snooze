import { describe, expect, it } from "vitest";
import type { DeliveryEntry } from "@/features/notifications/deliveries/types";
import { lastDeliverySummary } from "./lastDelivery";

describe("lastDeliverySummary", () => {
  it("returns null when there is no row", () => {
    expect(lastDeliverySummary(undefined)).toBeNull();
  });

  it("returns null when the row has no date", () => {
    expect(lastDeliverySummary({ status: "success" } as DeliveryEntry)).toBeNull();
  });

  it("reads a success row", () => {
    const row: DeliveryEntry = { date_epoch: 100, status: "success", action: "mail-oncall" };
    expect(lastDeliverySummary(row)).toEqual({
      variant: "success",
      epoch: 100,
      via: "mail-oncall",
      batchCount: null,
    });
  });

  it("reads an error row", () => {
    const row: DeliveryEntry = {
      date_epoch: 100,
      status: "error",
      action: "mail-oncall",
      error: "dial tcp",
    };
    expect(lastDeliverySummary(row)?.variant).toBe("error");
  });

  it("falls back to the notifier key when no action name is set", () => {
    const row: DeliveryEntry = { date_epoch: 100, status: "success", notifier: "webhook" };
    expect(lastDeliverySummary(row)?.via).toBe("webhook");
  });

  it("carries the batch member count when the row is a batch flush", () => {
    const row: DeliveryEntry = {
      date_epoch: 100,
      status: "success",
      action: "webhook-oncall",
      batch: true,
      alert_count: 7,
    };
    expect(lastDeliverySummary(row)?.batchCount).toBe(7);
  });

  it("has no batch count for an unbatched row", () => {
    const row: DeliveryEntry = {
      date_epoch: 100,
      status: "success",
      action: "mail-oncall",
      alert_count: 1,
    };
    expect(lastDeliverySummary(row)?.batchCount).toBeNull();
  });
});
