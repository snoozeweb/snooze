import type { ClusterStatus } from "./types";

export type ClusterVerdict = {
  tone: "ok" | "warning" | "critical";
  issues: number;
  label: string;
};

/**
 * clusterVerdict collapses the members + plugins tables into a single "is the
 * cluster healthy?" answer for the status-line banner: any member `down` →
 * critical, any degraded member or unloaded plugin → warning, otherwise ok.
 */
export function clusterVerdict(data: ClusterStatus): ClusterVerdict {
  const members = data.cluster?.members ?? [];
  const plugins = data.plugins ?? [];
  // Any non-ok member is an issue; anything worse than "degraded" (down, or an
  // unknown future status) is critical — mirroring how memberBadgeVariant
  // colours the rows, so the banner can never say "all clear" while a row is red.
  const notOk = members.filter((m) => m.status !== "ok").length;
  const critical = members.filter((m) => m.status !== "ok" && m.status !== "degraded").length;
  const unloaded = plugins.filter((p) => !p.loaded).length;
  const issues = notOk + unloaded;
  const tone: ClusterVerdict["tone"] = critical > 0 ? "critical" : issues > 0 ? "warning" : "ok";
  const label =
    issues === 0 ? "All systems operational" : `${issues} issue${issues === 1 ? "" : "s"} detected`;
  return { tone, issues, label };
}
