import { WEEKDAY_LABELS, type TimeConstraintsGroup } from "@/lib/timeconstraints/types";

export function summarizeTimeConstraints(g: TimeConstraintsGroup | undefined): string {
  if (!g) return "—";
  const parts: string[] = [];
  const wd = g.weekdays?.[0]?.weekdays ?? [];
  if (wd.length > 0) {
    if (wd.length === 7) {
      parts.push("every day");
    } else {
      parts.push(wd.map((i) => WEEKDAY_LABELS[i]).join(","));
    }
  }
  const tw = (g.time ?? []).map((t) => `${t.from ?? "00:00"}-${t.until ?? "23:59"}`).join(", ");
  if (tw) parts.push(tw);
  const dw = (g.datetime ?? []).length;
  if (dw > 0) parts.push(`${dw} date range${dw > 1 ? "s" : ""}`);
  if (parts.length === 0) return "always";
  return parts.join(" · ");
}
