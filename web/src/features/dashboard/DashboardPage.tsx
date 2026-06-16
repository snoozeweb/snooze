import { useCallback, useMemo } from "react";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { Card } from "@/shared/ui/Card";
import { useTheme } from "@/shared/hooks/useTheme";
import { BarChart } from "@/shared/chart/BarChart";
import { DistributionBar, type DistributionDatum } from "@/shared/chart/DistributionBar";
import { LineChart, type LineSeries } from "@/shared/chart/LineChart";
import { seriesColor } from "@/shared/chart/theme";
import { severityColor } from "@/lib/format/severity-color";
import { Environments } from "@/features/admin/environments/api";
import { tabById, type TabId } from "@/features/alerts/tabs";
import { Icon } from "@/shared/icons/Icon";
import type { IconName } from "@/shared/icons/icon-names";
import { useStats } from "./api";
import { TimeRangePicker } from "./TimeRangePicker";
import { presetToRange, type TimeRange } from "./time-range";
import { StatTiles, type TileId } from "./StatTiles";
import { DashboardSkeleton } from "./DashboardSkeleton";
import { ActivityFeed } from "./ActivityFeed";
import { alertsSearchForBucket, alertsSearchForRange } from "./bucket-utils";
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

function CardTitle({ icon, children }: { icon: IconName; children: string }) {
  return (
    <h2 className={styles.cardTitle}>
      <Icon name={icon} size={14} />
      {children}
    </h2>
  );
}

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

  const data = stats.data?.data;

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
      value,
      color: seriesColor(label, i),
    }));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [data, theme]);

  // Trend deltas vs the prior window. Only range-scoped summed series
  // (throttled, snoozed) get a delta; live snapshot tiles are point-in-time.
  const deltas: Partial<Record<TileId, number | null>> = useMemo(() => {
    const prev = prevStats.data?.data?.totals;
    if (!data || !prev) return {};
    return {
      throttled: pctDelta(sum(data.totals.by_throttled), sum(prev.by_throttled)),
      snoozed: pctDelta(sum(data.totals.by_snoozed), sum(prev.by_snoozed)),
    };
  }, [data, prevStats.data]);

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

  // State buckets map to an alerts lifecycle tab where one matches the
  // state value; otherwise fall back to a DSL search on `state`.
  const handleStateClick = (label: string) => {
    const tab = tabForState(label);
    if (tab) {
      void navigate({ to: "/web/alerts", search: { tab } });
    } else {
      void navigate({ to: "/web/alerts", search: { search: `state = ${label}` } });
    }
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
        <div className={styles.empty}>Failed to load dashboard stats.</div>
      ) : data ? (
        <>
          <StatTiles
            snapshot={data.snapshot}
            totals={data.totals}
            deltas={deltas}
            onTileClick={(tab) => void navigate({ to: "/web/alerts", search: { tab } })}
          />

          {/* Row 1: hero chart + activity feed */}
          <div className={styles.row1}>
            <Card padded>
              <CardTitle icon="activity">Alerts over time</CardTitle>
              {lineSeries.length === 0 ? (
                <div className={styles.empty}>No data.</div>
              ) : (
                <LineChart
                  series={lineSeries}
                  height={280}
                  toggleableLegend
                  theme={theme}
                  ariaLabel="Alerts over time by series"
                  onPointClick={handlePointClick}
                  onRangeSelect={handleRangeSelect}
                />
              )}
            </Card>
            <Card padded>
              <CardTitle icon="message-square">Recent activity</CardTitle>
              <ActivityFeed />
            </Card>
          </div>

          {/* Row 2: distribution bars + top hosts */}
          <div className={styles.row2}>
            <Card padded>
              <CardTitle icon="alert-triangle">By severity</CardTitle>
              {severityDist.length > 0 ? (
                <DistributionBar
                  data={severityDist}
                  ariaLabel="By severity"
                  onSegmentClick={handleSeverityClick}
                />
              ) : (
                <div className={styles.empty}>No data.</div>
              )}
            </Card>

            <Card padded>
              <CardTitle icon="layers">By environment</CardTitle>
              {environmentDist.length > 0 ? (
                <DistributionBar
                  data={environmentDist}
                  ariaLabel="By environment"
                  onSegmentClick={handleEnvClick}
                />
              ) : (
                <div className={styles.empty}>No data.</div>
              )}
            </Card>

            <Card padded>
              <CardTitle icon="check-circle">By state</CardTitle>
              {stateDist.length > 0 ? (
                <DistributionBar
                  data={stateDist}
                  ariaLabel="By state"
                  onSegmentClick={handleStateClick}
                />
              ) : (
                <div className={styles.empty}>No data.</div>
              )}
            </Card>

            <Card padded>
              <CardTitle icon="server">Top hosts</CardTitle>
              {Object.keys(data.totals.by_host).length > 0 ? (
                <BarChart
                  horizontal
                  sort="value"
                  theme={theme}
                  ariaLabel="Alert count by host"
                  height={Math.max(240, Object.keys(data.totals.by_host).length * 28)}
                  series={[
                    { label: "Hosts", color: seriesColor("Hosts"), data: data.totals.by_host },
                  ]}
                />
              ) : (
                <div className={styles.empty}>No data.</div>
              )}
            </Card>
          </div>

          {/* Row 3: 4 bar panels */}
          <div className={styles.row3}>
            <Card padded>
              <CardTitle icon="megaphone">Actions</CardTitle>
              {Object.keys(data.totals.by_action_success).length > 0 ||
              Object.keys(data.totals.by_action_failure).length > 0 ? (
                <BarChart
                  theme={theme}
                  ariaLabel="Action runs by name, successful versus failed"
                  series={[
                    {
                      label: "Successful",
                      color: seriesColor("Successful"),
                      data: data.totals.by_action_success,
                    },
                    {
                      label: "Failed",
                      color: seriesColor("Failed"),
                      data: data.totals.by_action_failure,
                    },
                  ]}
                />
              ) : (
                <div className={styles.empty}>No data.</div>
              )}
            </Card>

            <Card padded>
              <CardTitle icon="filter">Throttled by rule</CardTitle>
              {Object.keys(data.totals.by_throttled).length > 0 ? (
                <BarChart
                  theme={theme}
                  ariaLabel="Throttled alert count by rule"
                  series={[
                    {
                      label: "Throttled",
                      color: seriesColor("Throttled"),
                      data: data.totals.by_throttled,
                    },
                  ]}
                />
              ) : (
                <div className={styles.empty}>No data.</div>
              )}
            </Card>

            <Card padded>
              <CardTitle icon="bell-off">Snoozed by filter</CardTitle>
              {Object.keys(data.totals.by_snoozed).length > 0 ? (
                <BarChart
                  theme={theme}
                  ariaLabel="Snoozed alert count by filter"
                  series={[
                    {
                      label: "Snoozed",
                      color: seriesColor("Snoozed"),
                      data: data.totals.by_snoozed,
                    },
                  ]}
                />
              ) : (
                <div className={styles.empty}>No data.</div>
              )}
            </Card>

            <Card padded>
              <CardTitle icon="calendar">By weekday</CardTitle>
              {Object.values(weekdayData).some((v) => v > 0) ? (
                <BarChart
                  theme={theme}
                  ariaLabel="Alert count by weekday"
                  series={[{ label: "Alerts", color: seriesColor("Alerts"), data: weekdayData }]}
                />
              ) : (
                <div className={styles.empty}>No data.</div>
              )}
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

// Map a snapshot state value to an alerts lifecycle tab when one matches by
// the tab's preset EQUALS condition on `state`. Returns undefined otherwise.
function tabForState(state: string): TabId | undefined {
  const direct: Record<string, TabId> = {
    open: "alerts",
    ack: "ack",
    close: "closed",
    closed: "closed",
    shelved: "shelved",
    esc: "esc",
  };
  const tab = direct[state.toLowerCase()];
  // Guard against a future tab-id rename: only return a tab that still exists.
  return tab && tabById(tab).id === tab ? tab : undefined;
}

function bucketFromRange(range: TimeRange["range"]): number {
  if (range === "1d") return 3600; // 1h buckets (hourly server-side)
  if (range === "1w") return 3600; // 1h buckets
  if (range === "1m") return 21600; // 6h buckets
  if (range === "1y") return 86400; // 1d buckets
  return 3600;
}
