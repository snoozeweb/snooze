import { describe, expect, it } from "vitest";
import {
  DAEMON_SOURCES,
  hostOf,
  INJECTION_SOURCES,
  REST_SOURCE,
  sourcesForFamily,
  WEBHOOK_SOURCES,
} from "./injectionGuide";

describe("injectionGuide catalogue", () => {
  it("covers REST + 11 webhooks + 6 daemons", () => {
    expect(REST_SOURCE.id).toBe("rest");
    expect(WEBHOOK_SOURCES).toHaveLength(11);
    expect(DAEMON_SOURCES).toHaveLength(6);
    expect(INJECTION_SOURCES).toHaveLength(18);
  });

  it("every source has a summary and a docs slug, and unique ids", () => {
    const ids = new Set<string>();
    for (const s of INJECTION_SOURCES) {
      expect(s.summary.length).toBeGreaterThan(0);
      expect(s.docSlug).toMatch(/^general\/integrations\//);
      expect(ids.has(s.id)).toBe(false);
      ids.add(s.id);
    }
  });

  it("sourcesForFamily filters by family", () => {
    expect(sourcesForFamily("rest").map((s) => s.id)).toEqual(["rest"]);
    expect(sourcesForFamily("webhook")).toHaveLength(11);
    expect(sourcesForFamily("daemon")).toHaveLength(6);
    expect(sourcesForFamily("daemon")).toEqual(DAEMON_SOURCES);
  });

  it("REST snippet substitutes the live base URL", () => {
    expect(REST_SOURCE.snippet("https://snz.test")).toContain("https://snz.test/api/v1/alerts");
  });

  it("a webhook snippet is the receiver URL on the live host", () => {
    const grafana = WEBHOOK_SOURCES.find((s) => s.id === "grafana")!;
    expect(grafana.endpoint).toBe("POST /api/v1/webhook/grafana");
    expect(grafana.snippet("https://snz.test")).toBe("https://snz.test/api/v1/webhook/grafana");
    expect(grafana.family).toBe("webhook");
  });

  it("daemon snippets use the bare hostname, not the HTTP origin", () => {
    const syslog = DAEMON_SOURCES.find((s) => s.id === "syslog")!;
    expect(syslog.endpoint).toBeUndefined();
    expect(syslog.snippet("https://snooze.example.com")).toContain("snooze.example.com:514");
  });

  it("hostOf extracts a hostname and falls back on bad input", () => {
    expect(hostOf("https://snooze.example.com")).toBe("snooze.example.com");
    expect(hostOf("not a url")).toBe("not a url");
  });
});
