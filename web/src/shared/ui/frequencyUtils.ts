import type { Frequency } from "@/features/notifications/types";

export function summarizeFrequency(f: Frequency | undefined): string {
  if (!f || (!f.total && !f.delay && !f.every)) return "Once";
  const bits: string[] = [];
  if (f.total) bits.push(`×${f.total}`);
  if (f.every) bits.push(`every ${f.every}s`);
  if (f.delay) bits.push(`+${f.delay}s`);
  return bits.join(" ");
}
