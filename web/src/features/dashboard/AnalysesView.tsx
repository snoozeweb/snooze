// The dashboard's second view: the alerts somebody has already worked out the
// cause of — the ones still in play, and the closed ones until the housekeeper
// expires them.
//
// The alerts table answers "what is firing"; this answers "what has been
// explained, what do I do about it, and can any of it be handed to a machine".
//
// Every analysed alert is listed, not a top-N: the view is a work queue, and a
// ratio against the open backlog belongs to the Right-now tile that brought the
// reader here, not above a list it does not describe. As a work queue it is
// ordered by urgency by default — the severity the alert fired at, whether
// anybody has it in hand, how recently it fired — with "newest analysis" one
// click away for the reader who wants to see what the agent just wrote.
//
// Each row (AnalysisRow) reads collapsed as a triage line — verdict, headline,
// trust, the shape of the plan — and expands in place into the whole analysis.
// The rows are articles the keyboard can walk: J/K or the arrows move between
// them, Enter opens the alert, Space or E expands.
//
// Live, not windowed: the rows are the record store as it stands right now, so
// the page's time-range picker is not on screen in this view.
import { useCallback, useMemo, useRef, useState, type KeyboardEvent } from "react";
import {
  isDefaultVerdicts,
  type DashboardSearchParams,
  type VerdictId,
} from "@/app/dashboardSearch";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { Button } from "@/shared/ui/Button";
import { Card } from "@/shared/ui/Card";
import { InlineError } from "@/shared/ui/InlineError";
import { Skeleton } from "@/shared/ui/Skeleton";
import { isEditable } from "@/shared/hooks/useShortcut";
import { describeError } from "@/lib/api/errorMessage";
import { PLAN_STATUSES, planStatusHint, planStatusLabel } from "@/features/alerts/analysis/verdict";
import { ChoiceGroup, type Choice } from "./ChoiceGroup";
import { MultiChoiceGroup, type MultiChoicePreset } from "./MultiChoiceGroup";
import { PanelEmpty, PanelHint } from "./Panel";
import { AnalysisRow } from "./AnalysisRow";
import { useAnalysedOpenAlerts } from "./analyses-query";
import {
  ALL_VERDICTS,
  DEFAULT_FILTERS,
  FILTER_SEARCH_KEYS,
  filtersActive,
  filtersFromSearch,
  isClosed,
  matchesFilters,
  openAlertSearch,
  searchFromFilters,
  sortRows,
  toAnalysedRow,
  type AnalysesSort,
  type AnalysisFilters,
  type AutomatableFilter,
  type ClosedFilter,
  type ConfidenceFilter,
} from "./analysis-rows";
import styles from "./AnalysesView.module.css";

const CONFIDENCE_CHOICES: readonly Choice<ConfidenceFilter>[] = [
  { id: "any", label: "Any" },
  { id: "medium", label: "Medium+", hint: "Medium or high confidence" },
  { id: "high", label: "High", hint: "High confidence only" },
];

const AUTOMATABLE_CHOICES: readonly Choice<AutomatableFilter>[] = [
  { id: "any", label: "Any" },
  { id: "yes", label: "Yes", hint: "Plans safe to run unattended" },
  { id: "no", label: "No", hint: "Plans that need a human" },
];

const VERDICT_CHOICES: readonly Choice<VerdictId>[] = [
  ...PLAN_STATUSES.map((s) => ({ id: s, label: planStatusLabel(s), hint: planStatusHint(s) })),
  { id: "no_verdict", label: "No verdict", hint: "Analyses that state no verdict (older ones)" },
];

const VERDICT_PRESETS: readonly MultiChoicePreset<VerdictId>[] = [
  { label: "All", value: ALL_VERDICTS, hint: "Every verdict, finished ones included" },
];

const CLOSED_CHOICES: readonly Choice<ClosedFilter>[] = [
  { id: "show", label: "Show", hint: "Closed alerts stay listed until they expire" },
  { id: "hide", label: "Hide", hint: "Only alerts still in play" },
];

const SORT_CHOICES: readonly Choice<AnalysesSort>[] = [
  {
    id: "urgent",
    label: "Most urgent",
    hint: "Severity, then unacknowledged first, then most recently fired",
  },
  { id: "recent", label: "Newest analysis" },
];

// The view reads and writes the dashboard route's search (app/dashboardSearch.ts):
// `sort` and the four filter keys, each omitted while it is at its default.
type DashboardSearch = DashboardSearchParams;

// TanStack Router's navigate types are locked to the registered route tree at
// build time; the same cast DashboardPage uses, so a locally built route tree
// in tests type-checks too.
type NavigateFn = (opts: {
  to: string;
  search: ((prev: DashboardSearch | undefined) => DashboardSearch) | Record<string, unknown>;
  replace?: boolean;
}) => Promise<void>;

export function AnalysesView() {
  const navigate = useNavigate() as unknown as NavigateFn;
  const search = useSearch({ strict: false }) as unknown as DashboardSearch;
  const sort: AnalysesSort = search.sort === "recent" ? "recent" : "urgent";

  // The order and the filters ride in the URL — they decide what a shared link
  // opens on, and a reload keeps them. Each is omitted while it is at its
  // default, so a plain link still opens on the default view.
  const setSort = useCallback(
    (next: AnalysesSort) => {
      void navigate({
        to: "/web/dashboard",
        search: (prev) => {
          const { sort: _previous, ...rest } = prev ?? {};
          void _previous;
          return next === "recent" ? { ...rest, sort: "recent" } : rest;
        },
        // Pushed, not replaced: the order is in the URL, so it is a state the
        // operator can land on, and Back has to undo it like any other.
      });
    },
    [navigate],
  );

  const { confidence, automatable, verdict, closed } = search;
  const filters = useMemo(
    () => filtersFromSearch({ confidence, automatable, verdict, closed }),
    [confidence, automatable, verdict, closed],
  );
  const setFilters = useCallback(
    (next: AnalysisFilters | ((prev: AnalysisFilters) => AnalysisFilters)) => {
      const resolved = typeof next === "function" ? next(filters) : next;
      void navigate({
        to: "/web/dashboard",
        search: (prev) => {
          const rest: DashboardSearch = { ...(prev ?? {}) };
          for (const key of FILTER_SEARCH_KEYS) delete rest[key];
          return { ...rest, ...searchFromFilters(resolved) };
        },
        // Pushed, like the sort: a narrowing is a state the URL names, and
        // Back undoes it.
      });
    },
    [filters, navigate],
  );
  // Keyed by uid, not position, so an expanded row stays expanded across the
  // 30-second refetch and across a re-sort that moves it.
  const [expanded, setExpanded] = useState<ReadonlySet<string>>(() => new Set());
  // The row that holds the list's one tab stop (roving tabindex). Undefined —
  // or a uid that has since left the list — means the first row.
  const [activeUid, setActiveUid] = useState<string | undefined>(undefined);
  const articles = useRef(new Map<string, HTMLElement>());

  const query = useAnalysedOpenAlerts();
  const analysed = query.data?.meta.total ?? 0;

  const rows = useMemo(
    () => (query.data?.data ?? []).map(toAnalysedRow).filter((r) => r !== undefined),
    [query.data],
  );
  const visible = useMemo(
    () =>
      sortRows(
        rows.filter((r) => matchesFilters(r, filters)),
        sort,
      ),
    [rows, filters, sort],
  );
  const tabStop = visible.some((r) => r.uid === activeUid) ? activeUid : visible[0]?.uid;

  // The verdict is a field the contract only just grew; every analysis stored
  // before it has none. A filter whose every option but "No verdict" empties
  // the list is noise, so the group appears once some row states a verdict —
  // and stays while it is off its default, so a refetch can never hide a
  // filter that is in force. The default keeps "No verdict" ticked, so the
  // hidden group never hides a row. Same rule for the closed toggle.
  const showVerdict =
    !isDefaultVerdicts(filters.verdict) || rows.some((r) => r.status !== undefined);
  const showClosed = filters.closed !== "show" || rows.some(isClosed);

  const onToggle = useCallback((uid: string) => {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(uid)) next.delete(uid);
      else next.add(uid);
      return next;
    });
  }, []);
  const registerArticle = useCallback((uid: string, el: HTMLElement | null) => {
    if (el) articles.current.set(uid, el);
    else articles.current.delete(uid);
  }, []);

  function onListKeyDown(e: KeyboardEvent<HTMLUListElement>) {
    // The same guards as the alerts table's row keys: never steal a keystroke
    // typed into a field, and leave every modified key (Ctrl+K palette,
    // Ctrl+1…5 page nav, the browser's own) to whoever owns it.
    if (isEditable(e.target)) return;
    if (e.ctrlKey || e.metaKey || e.altKey) return;
    const article = (e.target as HTMLElement).closest<HTMLElement>("article[data-uid]");
    const uid = article?.dataset["uid"];
    if (uid === undefined) return;
    const key = e.key.length === 1 ? e.key.toLowerCase() : e.key;

    const step = key === "j" || key === "ArrowDown" ? 1 : key === "k" || key === "ArrowUp" ? -1 : 0;
    if (step !== 0) {
      const at = visible.findIndex((r) => r.uid === uid);
      const next = visible[Math.min(visible.length - 1, Math.max(0, at + step))];
      if (!next) return;
      e.preventDefault();
      setActiveUid(next.uid);
      articles.current.get(next.uid)?.focus();
      return;
    }

    // Enter / Space / E act on the ROW, so only while the row itself has
    // focus: on the heading link, the Expand button or a copy button inside
    // it, those keys already mean that control, and doubling them up would
    // toggle twice or open twice.
    if (e.target !== article) return;
    if (key === "Enter") {
      e.preventDefault();
      void navigate({
        to: "/web/alerts",
        search: {
          tab: "all",
          record: uid,
          pane: "analysis",
          search: openAlertSearch(uid, analysed),
        },
      });
    } else if (key === " " || key === "e") {
      e.preventDefault();
      onToggle(uid);
    }
  }

  if (query.isError) {
    return (
      <Card padded>
        <InlineError {...describeError(query.error, "Could not load the analysed alerts.")} />
      </Card>
    );
  }

  if (query.isPending) {
    return (
      <Card padded>
        <div className={styles.loading} aria-busy="true">
          <Skeleton height={16} width="30%" />
          <Skeleton height={14} />
          <Skeleton height={14} />
          <Skeleton height={14} width="80%" />
        </div>
      </Card>
    );
  }

  // "N of M" whenever the filters hide something — the default ones included,
  // since the default verdict set leaves the finished analyses out.
  const narrowed = visible.length !== rows.length;
  const offDefault = filtersActive(filters);

  return (
    <Card padded>
      {/* The page's h1 is "Dashboard"; the rows are h3s. The list's own level
          is heard, not seen — the view switch above already says it visibly. */}
      <h2 className={styles.srOnly}>Analysed alerts</h2>

      {/* Only on the one occasion the fetch ceiling bites: a truncated list
          that says nothing is a list claiming to be the whole backlog. */}
      {analysed > rows.length ? (
        <PanelHint>{`Showing ${rows.length} of ${analysed} analysed alerts`}</PanelHint>
      ) : null}

      {rows.length === 0 ? (
        <PanelEmpty
          title="No analyses yet"
          description="The alert-rca agent writes an analysis onto open alerts it investigates; you can also write one from an alert's Analysis tab."
        />
      ) : (
        <>
          <div className={styles.toolbar}>
            <div className={styles.filters}>
              <ChoiceGroup
                label="Confidence"
                options={CONFIDENCE_CHOICES}
                value={filters.confidence}
                onChange={(confidence) => setFilters((f) => ({ ...f, confidence }))}
              />
              <ChoiceGroup
                label="Automatable"
                options={AUTOMATABLE_CHOICES}
                value={filters.automatable}
                onChange={(automatable) => setFilters((f) => ({ ...f, automatable }))}
              />
              {showVerdict ? (
                <MultiChoiceGroup
                  label="Verdict"
                  options={VERDICT_CHOICES}
                  value={filters.verdict}
                  presets={VERDICT_PRESETS}
                  onChange={(next) => setFilters((f) => ({ ...f, verdict: next }))}
                />
              ) : null}
              {showClosed ? (
                <ChoiceGroup
                  label="Closed"
                  options={CLOSED_CHOICES}
                  value={filters.closed}
                  onChange={(next) => setFilters((f) => ({ ...f, closed: next }))}
                />
              ) : null}
              {/* Not while the list is empty: the empty state below carries the
                  same way out, and one "Reset" per screen is enough. */}
              {offDefault && visible.length > 0 ? (
                <Button size="sm" variant="ghost" onClick={() => setFilters(DEFAULT_FILTERS)}>
                  Reset to default
                </Button>
              ) : null}
            </div>
            <div className={styles.toolbarEnd}>
              {/* The result of the filters, where the filters are — and
                  announced, since a click that removes rows below the fold
                  is otherwise silent. */}
              <p className={styles.count} role="status">
                {narrowed
                  ? `${visible.length} of ${rows.length} analyses`
                  : `${rows.length} ${rows.length === 1 ? "analysis" : "analyses"}`}
              </p>
              <ChoiceGroup label="Sort" options={SORT_CHOICES} value={sort} onChange={setSort} />
            </div>
          </div>

          {visible.length === 0 ? (
            <PanelEmpty
              compact
              title="Nothing matches these filters"
              action={
                // Off the default, the way out is back to it. AT the default
                // it can still be empty — every analysis finished — and then
                // the way out is the verdicts the default leaves unticked.
                offDefault ? (
                  <Button size="sm" onClick={() => setFilters(DEFAULT_FILTERS)}>
                    Reset to default
                  </Button>
                ) : (
                  <Button
                    size="sm"
                    onClick={() => setFilters((f) => ({ ...f, verdict: ALL_VERDICTS }))}
                  >
                    Show all verdicts
                  </Button>
                )
              }
            />
          ) : (
            // The keys are the list's, not each row's: J/K move focus BETWEEN
            // rows, so the one handler that sees every row is the honest place
            // for them.
            // eslint-disable-next-line jsx-a11y/no-noninteractive-element-interactions -- delegated row navigation; each row article is the focus target.
            <ul className={styles.list} onKeyDown={onListKeyDown}>
              {visible.map((row) => (
                <AnalysisRow
                  key={row.uid}
                  row={row}
                  expanded={expanded.has(row.uid)}
                  tabStop={row.uid === tabStop}
                  analysedTotal={analysed}
                  onToggle={onToggle}
                  onActivate={setActiveUid}
                  registerArticle={registerArticle}
                />
              ))}
            </ul>
          )}
        </>
      )}
    </Card>
  );
}
