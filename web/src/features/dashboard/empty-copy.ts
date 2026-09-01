// Copy for the dashboard's empty panels, kept out of Panel.tsx so that file
// only exports components (fast-refresh boundary).
import type { PanelEmptyProps } from "./Panel";
import type { StatsCounters } from "./types";

/**
 * Wording for an empty counter-backed panel. Three states that look identical
 * on screen but mean entirely different things to an operator:
 *
 *   - metrics turned off  → this panel will never fill; go change a setting
 *   - nothing ever counted → fresh install; send an alert and it starts
 *   - quiet window        → the deployment is fine; widen the range
 *
 * On a fresh install several panels are empty at once, so `terse` drops the
 * explanation for the secondary ones: the reason is stated once, on the panel
 * the page leads with, instead of three times in one viewport.
 */
export function countersEmpty(
  counters: StatsCounters | undefined,
  windowLabel: string,
  subject: string,
  opts: { terse?: boolean } = {},
): PanelEmptyProps {
  const withDetail = (title: string, description: string): PanelEmptyProps =>
    opts.terse ? { title } : { title, description };

  if (counters && !counters.enabled) {
    return withDetail(
      "Counters are off",
      "Turn on metrics_enabled in Settings to chart ingest, throttling and snoozes.",
    );
  }
  if (counters && !counters.present) {
    return withDetail(
      "No counters yet",
      "They start filling as soon as the first alert is ingested.",
    );
  }
  return withDetail(
    `No ${subject} in this window`,
    `Nothing was recorded in the ${windowLabel.toLowerCase()}. Try a wider range.`,
  );
}
