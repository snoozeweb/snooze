// Epoch-second time formatting shared across the app.
//
// These helpers used to live in `src/features/alerts/format.ts`. They moved
// here so cross-feature primitives (e.g. `shared/ui/TimeCell`) can depend on
// them without reaching up into a feature folder. `features/alerts/format.ts`
// re-exports them, so alerts code keeps importing from its own module.

/**
 * formatRelativeTime emits "Xs / Xm / Xh / Xd" against now. Kept for the
 * audit timeline, comment timeline, and "last login" badge — places where
 * "8 minutes ago" reads better than the absolute trimDate format used by
 * the alerts table.
 */
export function formatRelativeTime(dateEpochSec: number | undefined): string {
  if (dateEpochSec === undefined || dateEpochSec === 0) return "—";
  const now = Math.floor(Date.now() / 1000);
  const diff = now - dateEpochSec;
  if (diff < 60) return diff <= 1 ? "just now" : `${diff}s`;
  if (diff < 3600) return `${Math.floor(diff / 60)}m`;
  if (diff < 86400) return `${Math.floor(diff / 3600)}h`;
  return `${Math.floor(diff / 86400)}d`;
}

const SHORT_MONTHS = [
  "Jan",
  "Feb",
  "Mar",
  "Apr",
  "May",
  "Jun",
  "Jul",
  "Aug",
  "Sep",
  "Oct",
  "Nov",
  "Dec",
] as const;

function ordinal(n: number): string {
  // English ordinal suffix: 1st, 2nd, 3rd, 4th–20th, 21st, 22nd, 23rd, 24th–30th, 31st.
  // The 11–13 carve-out is what makes 11th/12th/13th win over the units digit.
  const mod100 = n % 100;
  if (mod100 >= 11 && mod100 <= 13) return `${n}th`;
  switch (n % 10) {
    case 1:
      return `${n}st`;
    case 2:
      return `${n}nd`;
    case 3:
      return `${n}rd`;
    default:
      return `${n}th`;
  }
}

function pad2(n: number): string {
  return n < 10 ? `0${n}` : String(n);
}

/**
 * trimDate mirrors the moment.js-based formatter the old Snooze 1.x UI used
 * in web/src/utils/api.js (named "trimDate"). Same-day → "Today HH:mm",
 * different day within the current year → "MMM Do HH:mm" (e.g.
 * "Nov 5th 14:32"), different year → "MMM Do YYYY".
 *
 * The old UI exposed a `show_secs` toggle; we never used it on the alert
 * list, so the new helper sticks to minute resolution.
 */
export function trimDate(dateEpochSec: number | undefined): string {
  if (dateEpochSec === undefined || dateEpochSec === 0) return "—";
  const d = new Date(dateEpochSec * 1000);
  if (Number.isNaN(d.getTime())) return "—";
  const now = new Date();
  const hm = `${pad2(d.getHours())}:${pad2(d.getMinutes())}`;
  if (d.getFullYear() !== now.getFullYear()) {
    return `${SHORT_MONTHS[d.getMonth()]} ${ordinal(d.getDate())} ${d.getFullYear()}`;
  }
  if (d.getMonth() === now.getMonth() && d.getDate() === now.getDate()) {
    return `Today ${hm}`;
  }
  return `${SHORT_MONTHS[d.getMonth()]} ${ordinal(d.getDate())} ${hm}`;
}
