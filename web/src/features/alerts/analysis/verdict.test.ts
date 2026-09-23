import { describe, expect, it } from "vitest";
import {
  analysisIsStale,
  authorOf,
  groupSteps,
  planStatusLabel,
  readCaveats,
  splitSummary,
  isPlanStatus,
} from "./verdict";

describe("splitSummary", () => {
  it("keeps a short summary whole", () => {
    expect(splitSummary("Disk filled by nginx logs.")).toEqual({
      headline: "Disk filled by nginx logs.",
      body: "",
    });
  });

  it("uses detail as the body when the contract provides it", () => {
    expect(splitSummary("Rollout lacked runAsUser.", "Revision 24 never became ready.")).toEqual({
      headline: "Rollout lacked runAsUser.",
      body: "Revision 24 never became ready.",
    });
  });

  it("splits a long legacy summary at its first sentence", () => {
    const long =
      "Collateral damage from a fleet-wide api:3.7.0 rollout on ovh (2026-09-21 ~19:05-19:23 UTC): first wave lacked runAsUser. " +
      "Recreate strategy + 1 replica meant 0 availability for ~15min. Self-recovered; not app-specific.";
    const { headline, body } = splitSummary(long);
    expect(headline).toBe(
      "Collateral damage from a fleet-wide api:3.7.0 rollout on ovh (2026-09-21 ~19:05-19:23 UTC): first wave lacked runAsUser.",
    );
    expect(body).toBe(
      "Recreate strategy + 1 replica meant 0 availability for ~15min. Self-recovered; not app-specific.",
    );
  });

  it("does not split inside version numbers or abbreviations", () => {
    const s = "api:3.7.0 broke it e.g. on ovh. ".repeat(1) + "x".repeat(200);
    expect(splitSummary(s).headline.startsWith("api:3.7.0 broke it e.g. on ovh.")).toBe(true);
  });

  it("clips a long single sentence at a word boundary rather than splitting it", () => {
    const s = "word ".repeat(80).trim();
    const { headline, body } = splitSummary(s);
    expect(headline.endsWith("…")).toBe(true);
    expect(headline.length).toBeLessThanOrEqual(181);
    // The body continues from the clip instead of repeating the headline.
    expect(body.startsWith("…")).toBe(true);
    expect(headline.slice(0, -1) + " " + body.slice(1)).toBe(s);
  });
});

describe("readCaveats", () => {
  it("merges explicit caveats with legacy caveat: evidence lines", () => {
    expect(
      readCaveats({
        evidence: ["kubectl get rs: rev 24 had 0 ready", "caveat: pod events had expired"],
        caveats: ["causation is inferred"],
      }),
    ).toEqual({
      evidence: ["kubectl get rs: rev 24 had 0 ready"],
      caveats: ["causation is inferred", "pod events had expired"],
    });
  });

  it("tolerates loose JSON", () => {
    expect(readCaveats({ evidence: "nope", caveats: [1, "ok"] })).toEqual({
      evidence: [],
      caveats: ["ok"],
    });
  });
});

describe("groupSteps", () => {
  it("is ungrouped when no step says when", () => {
    const g = groupSteps<{ action: string; when?: string }>([{ action: "a" }, { action: "b" }]);
    expect(g.grouped).toBe(false);
    expect(g.now.map((e) => e.index)).toEqual([0, 1]);
    expect(g.followUp).toEqual([]);
  });

  it("splits now and follow-up, keeping original indexes", () => {
    const g = groupSteps<{ action: string; when?: string }>([
      { action: "a", when: "follow_up" },
      { action: "b", when: "now" },
      { action: "c" },
    ]);
    expect(g.grouped).toBe(true);
    expect(g.now.map((e) => e.index)).toEqual([1, 2]);
    expect(g.followUp.map((e) => e.index)).toEqual([0]);
  });
});

describe("plan status", () => {
  it("narrows and labels", () => {
    expect(isPlanStatus("self_resolved")).toBe(true);
    expect(isPlanStatus("fixed")).toBe(false);
    expect(planStatusLabel("self_resolved")).toBe("Self-resolved");
    expect(planStatusLabel("action_required")).toBe("Action required");
    expect(planStatusLabel("monitoring")).toBe("Monitoring");
  });
});

describe("authorOf", () => {
  it("reads the web UI tag as a human edit", () => {
    expect(authorOf({ source: "snooze-web", by: "florian" })).toEqual({
      kind: "human",
      tool: "",
      by: "florian",
    });
  });
  it("reads everything else as an AI analysis", () => {
    expect(authorOf({ source: "snooze-cli", by: "snooze" })).toEqual({
      kind: "agent",
      tool: "snooze-cli",
      by: "snooze",
    });
    expect(authorOf({ source: "alert-rca", by: "agent-bot" }).kind).toBe("agent");
    expect(authorOf(undefined).kind).toBe("agent");
  });
});

describe("analysisIsStale", () => {
  it("is stale when the alert fired again after the analysis", () => {
    expect(analysisIsStale(2_000_000_000, "2026-09-22T10:00:00Z")).toBe(true);
  });
  it("tolerates a minute of clock skew", () => {
    const at = "2026-09-22T10:00:00Z";
    const epoch = Date.parse(at) / 1000;
    expect(analysisIsStale(epoch + 30, at)).toBe(false);
    expect(analysisIsStale(epoch + 600, at)).toBe(true);
  });
  it("is never stale without both timestamps", () => {
    expect(analysisIsStale(undefined, "2026-09-22T10:00:00Z")).toBe(false);
    expect(analysisIsStale(1_800_000_000, undefined)).toBe(false);
  });
});
