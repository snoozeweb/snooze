import { useCallback, useEffect, useMemo, useRef } from "react";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { Button } from "@/shared/ui/Button";
import { Card } from "@/shared/ui/Card";
import { Tabs, TabList, TabTrigger, TabPanel } from "@/shared/ui/Tabs";
import { useTheme } from "@/shared/hooks/useTheme";
import { BarChart } from "@/shared/chart/BarChart";
import { DistributionBar, type DistributionDatum } from "@/shared/chart/DistributionBar";
import { LineChart, type LineSeries } from "@/shared/chart/LineChart";
import { seriesColor } from "@/shared/chart/theme";
import { severityColor } from "@/lib/format/severity-color";
import { Environments } from "@/features/admin/environments/api";
import { useActiveAlertCount } from "@/features/alerts/api";
import { useAnnounce } from "@/shared/a11y/LiveAnnouncer";
import { stateLabel } from "@/features/alerts/format";
import type { AlertState } from "@/features/alerts/types";
import { useStats } from "./api";
import { TimeRangePicker } from "./TimeRangePicker";
import { presetToRange, rangeLabel, type TimeRange } from "./time-range";
import { StatTiles, type TileId } from "./StatTiles";
import { DashboardSkeleton } from "./DashboardSkeleton";
import { ActivityFeed } from "./ActivityFeed";
import { NoiseRemoved } from "./NoiseRemoved";
import { PanelEmpty, PanelHint, PanelTitle } from "./Panel";
import { countersEmpty } from "./empty-copy";
import { alertsSearchForBucket, alertsSearchForRange } from "./bucket-utils";
import { capDistributions, formatBucketLabel } from "./chart-format";
import styles from "./DashboardPage.module.css";

// Series keys must match the exact strings the backend /stats emits in
// series[].counts — a backend rename silently drops the series here, making
// the mismatch visible. Order is canonical (matches seriesColor()'s map).
const LINE_SERIES_KEYS = [
  "Alerts",
  "Throttled",
  "Snoozed",
  "Notification sent",
  "Action error",
] as const;

const WEEKDAY_LABELS = ["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"] as const;
const WEEKDAY_KEYS = ["1", "2", "3", "4", "5", "6", "0"] as const;

// Search params backing the time-range picker. Mirrors the dashboard route's
// validateSearch (router.tsx): `range` preset key plus epoch-ms `from`/`to`
// for the custom window.
type DashboardSearch = {
  range?: TimeRange["range"];
  from?: number;
  to?: number;
};

// TanStack Router's navigate types are locked to the registered route tree at
// build time. Casting through unknown avoids type errors when the route is
// locally constructed in tests and still works when fully registered.
type NavigateFn = (opts: {
  to: string;
  search: (prev: DashboardSearch | undefined) => DashboardSearch;
}) => Promise<void>;

export function DashboardPage() {
  const navigate = useNavigate();
  const { theme } = useTheme();
  // useSearch with strict:false returns the validated search params; cast for local type.
  const search = useSearch({ strict: false }) as unknown as DashboardSearch;

  // Derive the picker value from the URL. No `range` param → today's default
  // (a 1d preset window). For a custom range we read the epoch-ms bounds back
  // out as ISO strings (TimeRange's wire shape); a preset recomputes its
  // rolling window from "now" on every render, exactly as the old state did.
  const range: TimeRange = useMemo(() => {
    const key = search.range ?? "1d";
    if (key === "custom") {
      return {
        range: "custom",
        from: search.from !== undefined ? new Date(search.from).toISOString() : "",
        to: search.to !== undefined ? new Date(search.to).toISOString() : "",
      };
    }
    const r = presetToRange(key);
    return { range: key, from: r.from, to: r.to };
  }, [search.range, search.from, search.to]);

  // Write picker changes to the URL. Presets drop from/to (the window is
  // recomputed from "now"); custom carries the bounds as epoch ms.
  const setRange = useCallback(
    (next: TimeRange) => {
      const nextSearch: DashboardSearch =
        next.range === "custom"
          ? {
              range: "custom",
              ...(next.from ? { from: Date.parse(next.from) } : {}),
              ...(next.to ? { to: Date.parse(next.to) } : {}),
            }
          : { range: next.range };
      void (navigate as unknown as NavigateFn)({
        to: "/web/dashboard",
        search: () => nextSearch,
      });
    },
    [navigate],
  );

  const bucket = bucketFromRange(range.range);
  const stats = useStats({ from: range.from, to: range.to, bucket });

  // Prior window of equal length immediately before [from, to], used purely
  // for the range-scoped trend deltas. Disabled until we have both bounds.
  const prior = useMemo(() => priorWindow(range.from, range.to), [range.from, range.to]);
  const prevStats = useStats({ from: prior.from, to: prior.to, bucket });

  // The headline live number is the queue the operator actually works: the
  // same ACTIVE_ALERTS preset behind the sidebar badge and the default alerts
  // tab. Reading it from that one query (React Query dedupes the key) is what
  // guarantees the tile, the badge and the table can never print three
  // different totals — `by_state.open` counts snoozed and shelved rows too.
  const activeCount = useActiveAlertCount(true);

  // This page repaints itself every 30s with no visible cue. "Needs attention"
  // is the one number an operator would want to hear change — it is the queue
  // they work — so announce only that, and only when a background poll moved
  // it. The -1 sentinel keeps the first load silent (arriving at a page is not
  // a change).
  const announce = useAnnounce();
  const prevAttentionRef = useRef<number>(-1);
  useEffect(() => {
    if (!activeCount.data) return;
    const now = activeCount.data.meta.total;
    const prev = prevAttentionRef.current;
    prevAttentionRef.current = now;
    if (prev < 0 || prev === now) return;
    announce(`Dashboard refreshed. Needs attention: ${now}, was ${prev}.`);
  }, [activeCount.data, announce]);

  const data = stats.data?.data;
  const counters = stats.data?.meta.counters;
  // Every counter-backed number on the page repeats this label; the live ones
  // say "Right now" instead. Without it the two halves read as contradictions.
  const windowLabel = rangeLabel(range.range);

  // Environment name → uid, so a "By environment" segment can drill into
  // the alerts page's ?env=<uid> contract. Cached app-wide via the resource.
  const envList = Environments.useList({ limit: 200, orderby: "tree_order", asc: true });
  const envUidByName = useMemo(() => {
    const m = new Map<string, string>();
    for (const e of envList.data?.data ?? []) {
      if (e.uid) m.set(e.name, e.uid);
    }
    return m;
  }, [envList.data]);

  // Re-resolve token-driven colours whenever the theme toggles.
  const lineSeries: LineSeries[] = useMemo(() => {
    const series = data?.series;
    if (!Array.isArray(series) || series.length === 0) return [];
    const keysPresent = new Set<string>();
    for (const b of series) {
      for (const k of Object.keys(b.counts ?? {})) keysPresent.add(k);
    }
    return LINE_SERIES_KEYS.filter((k) => keysPresent.has(k)).map((key, i) => ({
      label: key,
      color: seriesColor(key, i),
      data: series.map((b) => ({ x: b.t, y: b.counts[key] ?? 0 })),
    }));
    // theme is a dep so the resolved colours refresh on toggle.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [data, theme]);

  // Severity / environment / state distributions, coloured from tokens.
  const severityDist: DistributionDatum[] = useMemo(() => {
    const by = data?.totals.by_severity ?? {};
    return Object.entries(by).map(([label, value]) => ({
      label,
      value,
      color: severityColor(label),
    }));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [data, theme]);

  const environmentDist: DistributionDatum[] = useMemo(() => {
    const by = data?.totals.by_environment ?? {};
    return Object.entries(by).map(([label, value], i) => ({
      label,
      value,
      color: seriesColor(label, i),
    }));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [data, theme]);

  const stateDist: DistributionDatum[] = useMemo(() => {
    const by = data?.snapshot.by_state ?? {};
    return Object.entries(by).map(([label, value], i) => ({
      label,
      // Legend/title text uses the same noun as the state chip/tab/tile —
      // `label` stays the raw wire key so handleStateClick's `state = ${label}`
      // DSL filter keeps working.
      displayLabel: stateLabel(label as AlertState),
      value,
      color: seriesColor(label, i),
    }));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [data, theme]);

  // Trend deltas vs the prior window. Only the windowed counter tiles get one;
  // the live tiles are point-in-time and have no prior-window equivalent.
  const deltas: Partial<Record<TileId, number | null>> = useMemo(() => {
    const prev = prevStats.data?.data?.totals;
    if (!data || !prev) return {};
    return {
      throttled: pctDelta(sum(data.totals.by_throttled), sum(prev.by_throttled)),
      snoozed: pctDelta(sum(data.totals.by_snoozed), sum(prev.by_snoozed)),
    };
  }, [data, prevStats.data]);

  // Readable axis/tooltip labels at the grain of the selected range, replacing
  // the raw RFC3339 bucket timestamps.
  const formatX = useCallback((x: string) => formatBucketLabel(x, range.range), [range.range]);

  // Any series/point click navigates to the alerts in that time bucket.
  const handlePointClick = (_seriesLabel: string, x: string) => {
    void navigate({
      to: "/web/alerts",
      search: { search: alertsSearchForBucket(x, bucket) },
    });
  };

  // Dragging a range across the chart navigates to the alerts spanning the
  // whole dragged window (first → last bucket).
  const handleRangeSelect = (fromX: string, toX: string) => {
    void navigate({
      to: "/web/alerts",
      search: { search: alertsSearchForRange(fromX, toX, bucket) },
    });
  };

  const handleSeverityClick = (label: string) => {
    void navigate({ to: "/web/alerts", search: { search: `severity = ${label}` } });
  };

  // A state segment drills into exactly the rows it counted: the "All" tab
  // (no lifecycle preset) plus a DSL filter on the state itself. Routing to a
  // lifecycle tab instead would land on a different number — the default
  // "Alerts" tab, for one, also hides snoozed and shelved rows.
  const handleStateClick = (label: string) => {
    void navigate({ to: "/web/alerts", search: { tab: "all", search: `state = ${label}` } });
  };

  const handleEnvClick = (label: string) => {
    const uid = envUidByName.get(label);
    if (uid) {
      void navigate({ to: "/web/alerts", search: { env: uid } });
    } else {
      // No matching environment resource — best-effort DSL fallback.
      void navigate({ to: "/web/alerts", search: { search: `environment = ${label}` } });
    }
  };

  const weekdayData: Record<string, number> = useMemo(() => {
    const wd = data?.weekday ?? {};
    const out: Record<string, number> = {};
    for (let i = 0; i < WEEKDAY_LABELS.length; i++) {
      out[WEEKDAY_LABELS[i]!] = wd[WEEKDAY_KEYS[i]!] ?? 0;
    }
    return out;
  }, [data]);

  // Cap the high-cardinality Actions panel to its top slice so it stays
  // legible on busy instances (dozens of actions), rendered horizontal and
  // height-scaled rather than as fixed-height vertical slivers.
  const actionBars = useMemo(() => {
    const { keys, capped, total } = capDistributions(
      [data?.totals.by_action_success ?? {}, data?.totals.by_action_failure ?? {}],
      BAR_PANEL_CAP,
    );
    return { count: keys.length, total, success: capped[0]!, failure: capped[1]! };
  }, [data]);

  // The primary panel (Noise removed) carries the full explanation; the rest
  // state the fact only, so a fresh install doesn't repeat one sentence three
  // times in a single viewport.
  const windowedEmpty = (subject: string) =>
    countersEmpty(counters, windowLabel, subject, { terse: true });

  return (
    <div className={styles.page}>
      {/* Header */}
      <div className={styles.header}>
        <h1 className={styles.title}>Dashboard</h1>
        <TimeRangePicker value={range} onChange={setRange} />
      </div>

      {/* First-load skeleton (isPending only — background refetch keeps prior data). */}
      {stats.isPending ? (
        <DashboardSkeleton />
      ) : stats.isError ? (
        <Card padded>
          <PanelEmpty
            title="Couldn't load the dashboard"
            description={stats.error.message || "The stats request failed."}
          />
          <div className={styles.errorAction}>
            <Button variant="secondary" onClick={() => void stats.refetch()}>
              Try again
            </Button>
          </div>
        </Card>
      ) : data ? (
        <>
          <StatTiles
            snapshot={data.snapshot}
            totals={data.totals}
            needsAttention={activeCount.data?.meta.total ?? 0}
            windowLabel={windowLabel}
            deltas={deltas}
            onTileClick={(tab) => void navigate({ to: "/web/alerts", search: { tab } })}
          />

          {/* Row 1 — the two questions the page exists to answer: how much
              noise was removed, and what the stream looked like over time. */}
          <div className={styles.rowPrimary}>
            <Card padded>
              <NoiseRemoved
                ingested={data.snapshot.total_hits}
                throttled={data.totals.by_throttled}
                snoozed={data.totals.by_snoozed}
                windowLabel={windowLabel}
                {...(counters ? { counters } : {})}
                theme={theme}
              />
            </Card>

            <Card padded>
              <PanelTitle icon="activity">Alerts over time</PanelTitle>
              <PanelHint>{`${windowLabel}, events per ${bucketLabel(bucket)}`}</PanelHint>
              {lineSeries.length === 0 ? (
                <PanelEmpty {...windowedEmpty("events")} />
              ) : (
                <LineChart
                  series={lineSeries}
                  height={300}
                  toggleableLegend
                  theme={theme}
                  ariaLabel="Alerts over time by series"
                  formatX={formatX}
                  onPointClick={handlePointClick}
                  onRangeSelect={handleRangeSelect}
                />
              )}
            </Card>
          </div>

          {/* Row 2 — supporting detail: the live state of the queue, the
              secondary breakdowns folded into one tabbed panel, and who did
              what recently. */}
          <div className={styles.rowSecondary}>
            <Card padded>
              <PanelTitle icon="check-circle">By state</PanelTitle>
              <PanelHint>Right now, every alert including snoozed and shelved</PanelHint>
              {stateDist.length > 0 ? (
                <DistributionBar
                  data={stateDist}
                  ariaLabel="By state"
                  onSegmentClick={handleStateClick}
                />
              ) : (
                <PanelEmpty
                  title="No alerts yet"
                  description="Nothing has been ingested, so there is no queue to show."
                />
              )}
            </Card>

            <Card padded>
              <PanelTitle icon="layers">Breakdowns</PanelTitle>
              <PanelHint>{`${windowLabel}, events by dimension`}</PanelHint>
              <Tabs defaultValue="severity">
                <TabList>
                  <TabTrigger value="severity">Severity</TabTrigger>
                  <TabTrigger value="environment">Environment</TabTrigger>
                  <TabTrigger value="hosts">Hosts</TabTrigger>
                  <TabTrigger value="actions">Actions</TabTrigger>
                  <TabTrigger value="weekday">Weekday</TabTrigger>
                </TabList>

                <TabPanel value="severity">
                  {severityDist.length > 0 ? (
                    <DistributionBar
                      data={severityDist}
                      ariaLabel="By severity"
                      onSegmentClick={handleSeverityClick}
                    />
                  ) : (
                    <PanelEmpty {...windowedEmpty("events")} />
                  )}
                </TabPanel>

                <TabPanel value="environment">
                  {environmentDist.length > 0 ? (
                    <DistributionBar
                      data={environmentDist}
                      ariaLabel="By environment"
                      onSegmentClick={handleEnvClick}
                    />
                  ) : (
                    <PanelEmpty {...windowedEmpty("events")} />
                  )}
                </TabPanel>

                <TabPanel value="hosts">
                  {Object.keys(data.totals.by_host).length > 0 ? (
                    <BarChart
                      horizontal
                      sort="value"
                      theme={theme}
                      ariaLabel="Alert count by host"
                      height={Math.max(220, Object.keys(data.totals.by_host).length * 28)}
                      series={[
                        { label: "Hosts", color: seriesColor("Hosts"), data: data.totals.by_host },
                      ]}
                    />
                  ) : (
                    <PanelEmpty {...windowedEmpty("events")} />
                  )}
                </TabPanel>

                <TabPanel value="actions">
                  {actionBars.count > 0 ? (
                    <>
                      {actionBars.total > BAR_PANEL_CAP ? (
                        <PanelHint>{`Top ${BAR_PANEL_CAP} of ${actionBars.total}`}</PanelHint>
                      ) : null}
                      <BarChart
                        horizontal
                        sort="value"
                        theme={theme}
                        ariaLabel="Action runs by name, successful versus failed"
                        height={Math.max(220, actionBars.count * 44)}
                        series={[
                          {
                            label: "Successful",
                            color: seriesColor("Successful"),
                            data: actionBars.success,
                          },
                          {
                            label: "Failed",
                            color: seriesColor("Failed"),
                            data: actionBars.failure,
                          },
                        ]}
                      />
                    </>
                  ) : (
                    <PanelEmpty {...windowedEmpty("action runs")} />
                  )}
                </TabPanel>

                <TabPanel value="weekday">
                  {Object.values(weekdayData).some((v) => v > 0) ? (
                    <BarChart
                      theme={theme}
                      ariaLabel="Alert count by weekday"
                      series={[
                        { label: "Alerts", color: seriesColor("Alerts"), data: weekdayData },
                      ]}
                    />
                  ) : (
                    <PanelEmpty {...windowedEmpty("events")} />
                  )}
                </TabPanel>
              </Tabs>
            </Card>

            <Card padded>
              <PanelTitle icon="message-square">Recent activity</PanelTitle>
              <PanelHint>Right now, newest first</PanelHint>
              <ActivityFeed />
            </Card>
          </div>
        </>
      ) : null}
    </div>
  );
}

const sum = (m: Record<string, number>) => Object.values(m).reduce((a, b) => a + b, 0);

/**
 * Percentage change of `current` vs `previous`. Returns null when the prior
 * period is empty/zero (no meaningful baseline → the tile omits the badge),
 * or when the values are identical.
 */
function pctDelta(current: number, previous: number): number | null {
  if (previous <= 0) return null;
  if (current === previous) return null;
  return ((current - previous) / previous) * 100;
}

/**
 * The window of equal length ending exactly where the current one begins:
 * prevFrom = from - (to - from), prevTo = from. Returns empty strings when
 * either bound is missing (custom range not yet picked) so useStats stays
 * idle-but-harmless.
 */
function priorWindow(from: string, to: string): { from: string; to: string } {
  const f = Date.parse(from);
  const t = Date.parse(to);
  if (Number.isNaN(f) || Number.isNaN(t) || t <= f) return { from: "", to: "" };
  const span = t - f;
  return { from: new Date(f - span).toISOString(), to: new Date(f).toISOString() };
}

// Top-slice size for the high-cardinality Actions panel. Matches the spirit of
// the server-side top-10 by_host cap.
const BAR_PANEL_CAP = 12;

function bucketFromRange(range: TimeRange["range"]): number {
  if (range === "1d") return 3600; // 1h buckets (hourly server-side)
  if (range === "1w") return 3600; // 1h buckets
  if (range === "1m") return 21600; // 6h buckets
  if (range === "1y") return 86400; // 1d buckets
  return 3600;
}

/** Bucket size as words, for the hint line under the time-series title. */
function bucketLabel(seconds: number): string {
  if (seconds % 86400 === 0) {
    const d = seconds / 86400;
    return d === 1 ? "day" : `${d} days`;
  }
  const h = Math.max(Math.round(seconds / 3600), 1);
  return h === 1 ? "hour" : `${h} hours`;
}
