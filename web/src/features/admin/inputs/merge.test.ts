import { describe, it, expect } from "vitest";
import { mergeCatalogueWithActivity } from "./merge";
import type { InputActivity } from "./types";
import type { InjectionSource } from "@/features/alerts/injectionGuide";

const catalogue: InjectionSource[] = [
  { id: "rest", name: "REST API", family: "rest", summary: "", snippet: () => "", docSlug: "general/integrations/rest-api" },
  { id: "grafana", name: "Grafana", family: "webhook", summary: "", snippet: () => "", docSlug: "general/integrations/grafana", sourceKeys: ["grafana"] },
  { id: "syslog", name: "Syslog", family: "daemon", summary: "", snippet: () => "", docSlug: "general/integrations/syslog", sourceKeys: ["syslog"] },
];

const activity: InputActivity[] = [
  { source: "grafana", last_epoch: 1050, count: 2 }, // matches grafana
  { source: "graylog", last_epoch: 900, count: 1 },  // uncatalogued → other
];

describe("mergeCatalogueWithActivity", () => {
  const { inputs, other } = mergeCatalogueWithActivity(catalogue, activity);

  it("attaches activity to a matched catalogue input", () => {
    const g = inputs.find((r) => r.id === "grafana")!;
    expect(g.lastEpoch).toBe(1050);
    expect(g.count).toBe(2);
  });
  it("leaves an idle input without an epoch (renders 'never')", () => {
    const s = inputs.find((r) => r.id === "syslog")!;
    expect(s.lastEpoch).toBeUndefined();
  });
  it("flags REST as restNoActivity (renders '—')", () => {
    const rest = inputs.find((r) => r.id === "rest")!;
    expect(rest.restNoActivity).toBe(true);
    expect(rest.lastEpoch).toBeUndefined();
  });
  it("collects uncatalogued observed sources into other", () => {
    expect(other).toHaveLength(1);
    expect(other[0]!.id).toBe("graylog");
    expect(other[0]!.lastEpoch).toBe(900);
    expect(other[0]!.catalogue).toBe(false);
  });
});
