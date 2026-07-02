import type { InjectionSource } from "@/features/alerts/injectionGuide";
import type { InputActivity, InputRow } from "./types";

/**
 * Join the static input catalogue to observed per-source activity.
 * Matching is case-insensitive on `sourceKeys`. Catalogue inputs with no match
 * come back without `lastEpoch` (the table renders "never"); REST is flagged
 * `restNoActivity` (its source is caller-defined). Observed sources not claimed
 * by any catalogue entry are returned in `other`.
 */
export function mergeCatalogueWithActivity(
  catalogue: InjectionSource[],
  activity: InputActivity[],
): { inputs: InputRow[]; other: InputRow[] } {
  const bySource = new Map<string, InputActivity>();
  for (const a of activity) bySource.set(a.source.toLowerCase(), a);

  const consumed = new Set<string>();
  const inputs: InputRow[] = catalogue.map((s) => {
    const keys = (s.sourceKeys ?? []).map((k) => k.toLowerCase());
    const matches: InputActivity[] = [];
    for (const k of keys) {
      const hit = bySource.get(k);
      if (hit) {
        matches.push(hit);
        consumed.add(k);
      }
    }
    const row: InputRow = {
      id: s.id,
      name: s.name,
      family: s.family,
      docSlug: s.docSlug,
      catalogue: true,
    };
    if (s.family === "rest") {
      row.restNoActivity = true;
    } else if (matches.length > 0) {
      row.lastEpoch = Math.max(...matches.map((m) => m.last_epoch));
      row.count = matches.reduce((n, m) => n + m.count, 0);
    }
    return row;
  });

  const other: InputRow[] = [];
  for (const a of activity) {
    if (consumed.has(a.source.toLowerCase())) continue;
    other.push({
      id: a.source,
      name: a.source,
      family: "other",
      lastEpoch: a.last_epoch,
      count: a.count,
      catalogue: false,
    });
  }
  return { inputs, other };
}
