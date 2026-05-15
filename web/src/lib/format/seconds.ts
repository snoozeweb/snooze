// Format a duration expressed in seconds as a short human label
// ("2d 4h", "45m", "30s", "forever" for 0, "—" for negative/undefined).
// Mirrors the legacy Vue `pp_countdown` helper (utils/api.js).
export function secondsToHuman(seconds: number | undefined | null): string {
  if (seconds === undefined || seconds === null) return "—";
  if (!Number.isFinite(seconds)) return "—";
  if (seconds < 0) return "—";
  if (seconds === 0) return "forever";
  const s = Math.floor(seconds);
  const parts: string[] = [];
  const d = Math.floor(s / 86400);
  const h = Math.floor((s % 86400) / 3600);
  const m = Math.floor((s % 3600) / 60);
  const sec = s % 60;
  if (d > 0) parts.push(`${d}d`);
  if (h > 0) parts.push(`${h}h`);
  if (m > 0) parts.push(`${m}m`);
  if (sec > 0 || parts.length === 0) parts.push(`${sec}s`);
  return parts.slice(0, 2).join(" ");
}
