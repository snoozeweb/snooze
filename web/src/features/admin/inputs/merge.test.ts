import { describe, it, expect } from "vitest";
import { mergeCatalogueWithActivity } from "./merge";
import type { InputActivity } from "./types";
import type { InjectionSource } from "@/features/alerts/injectionGuide";

const catalogue: InjectionSource[] = [
  {
    id: "rest",
    name: "REST API",
    family: "rest",
    summary: "",
    snippet: () => "",
    docSlug: "general/integrations/rest-api",
  },
  {
    id: "grafana",
    name: "Grafana",
    family: "webhook",
    summary: "",
    snippet: () => "",
    docSlug: "general/integrations/grafana",
    sourceKeys: ["grafana"],
  },
  {
    id: "syslog",
    name: "Syslog",
    family: "daemon",
    summary: "",
    snippet: () => "",
    docSlug: "general/integrations/syslog",
    sourceKeys: ["syslog"],
  },
];

const activity: InputActivity[] = [
  { source: "grafana", last_epoch: 1050, count: 2 }, // matches grafana
  { source: "graylog", last_epoch: 900, count: 1 }, // uncatalogued → other
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

  it("matches case-insensitively (activity source cased differently than sourceKeys)", () => {
    const cat: InjectionSource[] = [
      {
        id: "alertmanager",
        name: "Alertmanager",
        family: "webhook",
        summary: "",
        snippet: () => "",
        docSlug: "general/integrations/alertmanager",
        sourceKeys: ["alertmanager"],
      },
    ];
    const act: InputActivity[] = [{ source: "AlertManager", last_epoch: 500, count: 3 }];
    const { inputs, other } = mergeCatalogueWithActivity(cat, act);
    expect(inputs.find((r) => r.id === "alertmanager")!.lastEpoch).toBe(500);
    expect(inputs.find((r) => r.id === "alertmanager")!.count).toBe(3);
    expect(other).toHaveLength(0); // consumed, not orphaned
  });

  it("aggregates MAX epoch and SUM count across multiple sourceKeys", () => {
    const cat: InjectionSource[] = [
      {
        id: "multi",
        name: "Multi",
        family: "webhook",
        summary: "",
        snippet: () => "",
        docSlug: "x",
        sourceKeys: ["a", "b"],
      },
    ];
    const act: InputActivity[] = [
      { source: "a", last_epoch: 100, count: 2 },
      { source: "b", last_epoch: 300, count: 5 },
    ];
    const { inputs } = mergeCatalogueWithActivity(cat, act);
    const row = inputs.find((r) => r.id === "multi")!;
    expect(row.lastEpoch).toBe(300);
    expect(row.count).toBe(7);
  });

  it("lets one activity source populate two catalogue entries without orphaning it", () => {
    const cat: InjectionSource[] = [
      {
        id: "e1",
        name: "E1",
        family: "webhook",
        summary: "",
        snippet: () => "",
        docSlug: "x",
        sourceKeys: ["shared"],
      },
      {
        id: "e2",
        name: "E2",
        family: "webhook",
        summary: "",
        snippet: () => "",
        docSlug: "y",
        sourceKeys: ["shared"],
      },
    ];
    const act: InputActivity[] = [{ source: "shared", last_epoch: 900, count: 1 }];
    const { inputs, other } = mergeCatalogueWithActivity(cat, act);
    expect(inputs.find((r) => r.id === "e1")!.lastEpoch).toBe(900);
    expect(inputs.find((r) => r.id === "e2")!.lastEpoch).toBe(900);
    expect(other).toHaveLength(0);
  });
});
