// DeliveryTimeline — the delivery history of one notification, one action or
// one alert, newest first.
//
// Lives in the ~600px docked inspector, so it is a single column: a count
// header with All/Failed/Batched chips, an <ol> of rows on a rail, and the
// audit timeline's paging controls. Everything colour comes from tokens; the
// only two hues it spends are the rail dot's ok/critical pair, which the
// status Badge repeats in words.
import { useEffect, useMemo, useState, type ReactNode } from "react";
import { Button } from "@/shared/ui/Button";
import { IconButton } from "@/shared/ui/IconButton";
import { InlineError } from "@/shared/ui/InlineError";
import { Skeleton } from "@/shared/ui/Skeleton";
import { Icon } from "@/shared/icons/Icon";
import { describeError } from "@/lib/api/errorMessage";
import { deliveryScopeKey, useDeliveries, useDeliverySummary, useFailedDeliveryCount } from "./api";
import { rangeChipLabel } from "./format";
import { DeliveryRow } from "./DeliveryRow";
import type { DeliveryFilter, DeliveryRange, DeliveryVariant } from "./types";
import styles from "./DeliveryTimeline.module.css";

const PAGE_SIZE_OPTIONS = [10, 25, 50] as const;
const DEFAULT_PAGE_SIZE = 10;

/** The local narrowing chips. Client state — the scope lives in the URL. */
type ChipId = "all" | "failed" | "batched";

const CHIPS: { id: ChipId; label: string }[] = [
  { id: "all", label: "All" },
  { id: "failed", label: "Failed" },
  { id: "batched", label: "Batched" },
];

const CHIP_EMPTY: Record<Exclude<ChipId, "all">, string> = {
  failed: "No failed deliveries.",
  batched: "No batched deliveries.",
};

export type DeliveryTimelineProps = {
  /** What we are listing deliveries for. */
  filter: DeliveryFilter;
  /** Which identifiers each row prints — set by the hosting inspector. */
  variant: DeliveryVariant;
  /**
   * Rendered when the scope has no deliveries at all. The copy depends on
   * *why* (no actions configured / disabled / nothing has matched yet), which
   * only the parent knows — see Task 11.
   */
  emptyState: ReactNode;
  /** Poll every 30 s. Pass true only while the drawer is open. */
  live?: boolean;
  /** Window from a dashboard deep link. Shown as a dismissable chip. */
  initialRange?: DeliveryRange;
  /** Called by the range chip's ✕. Omit and the chip is not dismissable. */
  onRangeClear?: () => void;
};

export function DeliveryTimeline({
  filter,
  variant,
  emptyState,
  live,
  initialRange,
  onRangeClear,
}: DeliveryTimelineProps) {
  const [chip, setChip] = useState<ChipId>("all");
  const [pageSize, setPageSize] = useState<number>(DEFAULT_PAGE_SIZE);
  const [page, setPage] = useState<number>(1);

  // The window from the deep link wins over anything the parent baked into
  // `filter`, so dismissing the chip (which clears `initialRange` upstream)
  // actually widens the list.
  const range = initialRange ?? filter.range;

  // The docked inspector RETARGETS this component instead of remounting it —
  // clicking prev/next in the drawer swaps `filter` under a live tree. Paging
  // state is meaningless across that swap: page 2 of a notification with 30
  // deliveries becomes an out-of-range offset on one with 1, which renders as
  // "no deliveries" under a header saying "1 delivery", with the paging
  // controls unmounted so there is no way back. Reset during render (the
  // documented React pattern) rather than in an effect, so the query never
  // fires with the stale offset in the first place.
  const scopeKey = `${deliveryScopeKey(filter)}|${range ? `${range.from}-${range.to}` : ""}`;
  const [lastScopeKey, setLastScopeKey] = useState(scopeKey);
  if (lastScopeKey !== scopeKey) {
    setLastScopeKey(scopeKey);
    setPage(1);
    setChip("all");
  }

  const baseFilter = useMemo<DeliveryFilter>(
    () => (range ? { ...filter, range } : filter),
    [filter, range],
  );
  const listFilter = useMemo<DeliveryFilter>(() => {
    if (chip === "failed") return { ...baseFilter, status: "error" };
    if (chip === "batched") return { ...baseFilter, batchOnly: true };
    return baseFilter;
  }, [baseFilter, chip]);

  const liveOpts = live === true ? { live: true } : undefined;
  const query = useDeliveries(
    listFilter,
    { limit: pageSize, offset: (page - 1) * pageSize },
    liveOpts,
  );

  // Counts follow the scope, not the chips: the header is a fixed statement
  // about the object ("142 deliveries · 3 failed"), and it must not change
  // when the operator narrows the list below it.
  //
  // With no chip active the list query IS the scope query, so its `meta.total`
  // already answers "how many" — asking a second time would be a duplicate
  // full scan. Only a narrowed list needs its own scope-total request, and
  // that one dedupes with the alert inspector's tab-count query (same key).
  //
  // Both count probes take the same `live` flag as the list: they are part of
  // the same header sentence, and a rows-poll that leaves "142 deliveries ·
  // 3 failed" frozen (and the alert inspector's tab badge with it, which
  // dedupes onto this key) states two different moments in one line.
  const scopeSummary = useDeliverySummary(baseFilter, { ...liveOpts, enabled: chip !== "all" });
  const failedCount = useFailedDeliveryCount(baseFilter, liveOpts);
  const listTotal = query.data?.meta.total ?? 0;
  // Placeholder rows outlive the narrowing that produced them: clearing the
  // Failed chip reuses the previous (failed) page as the placeholder for the
  // un-narrowed request, so `meta.total` is the *narrowed* count until the
  // real response lands. Reading it would flash "3 deliveries" under an All
  // chip. A skeleton for one frame is the honest answer.
  const headerTotal =
    chip === "all"
      ? query.isPlaceholderData
        ? undefined
        : query.data?.meta.total
      : scopeSummary.total;

  function selectChip(next: ChipId) {
    setChip(next);
    setPage(1);
  }

  const rows = query.data?.data ?? [];
  const pageCount = Math.max(1, Math.ceil(listTotal / pageSize));

  // Belt to the reset's braces: a page can also fall off the end without the
  // scope changing (rows aged out by retention, or the live refetch shrinking
  // the list under the operator).
  useEffect(() => {
    if (query.isSuccess && page > pageCount) setPage(pageCount);
  }, [query.isSuccess, page, pageCount]);

  // The paging controls are also the page-SIZE controls, so they have to stay
  // reachable whenever any of them is doing something: more than one page, a
  // page other than the first, or a non-default size the operator picked (a
  // "50 per page" that then narrows to 3 failed rows must not strand them).
  const showControls = pageCount > 1 || page > 1 || pageSize !== DEFAULT_PAGE_SIZE;

  // A scope that has never delivered anything has nothing to count and
  // nothing to narrow: "0 deliveries" above three filter chips above an empty
  // state that already says "No deliveries yet" is three ways of saying the
  // same nothing. The header comes back as soon as there is a row — and it
  // stays while a chip is active, because the chips are the only way back.
  const scopeEmpty = chip === "all" && rows.length === 0 && headerTotal === 0;

  return (
    <div className={styles.timeline}>
      <div className={styles.header} hidden={scopeEmpty || undefined}>
        <p className={styles.counts}>
          {headerTotal === undefined ? (
            <Skeleton width={140} height={14} />
          ) : (
            <>
              <span className={styles.countsTotal}>
                {headerTotal} {headerTotal === 1 ? "delivery" : "deliveries"}
              </span>
              {failedCount.failed ? (
                <span className={styles.countsFailed}>
                  {" · "}
                  {failedCount.failed} failed
                </span>
              ) : null}
            </>
          )}
        </p>
        <div className={styles.chips} role="group" aria-label="Filter deliveries">
          {CHIPS.map((c) => (
            <button
              key={c.id}
              type="button"
              className={styles.chip}
              aria-pressed={chip === c.id}
              data-active={chip === c.id || undefined}
              onClick={() => selectChip(c.id)}
            >
              {c.label}
            </button>
          ))}
        </div>
      </div>

      {initialRange ? (
        <div className={styles.rangeStrip}>
          <span className={styles.rangeChip}>
            <span className={styles.rangeLabel}>Window</span>
            <span className={styles.rangeValue}>{rangeChipLabel(initialRange)}</span>
            {onRangeClear ? (
              <button
                type="button"
                className={styles.rangeRemove}
                aria-label={`Remove window filter: ${rangeChipLabel(initialRange)}`}
                onClick={onRangeClear}
              >
                <Icon name="x" size={12} />
              </button>
            ) : null}
          </span>
        </div>
      ) : null}

      {query.isPending ? (
        <div className={styles.skeletons} aria-busy="true">
          {Array.from({ length: 3 }).map((_, i) => (
            <div key={i} className={styles.skeletonRow}>
              <span className={styles.skeletonDot} />
              <Skeleton height={34} />
            </div>
          ))}
        </div>
      ) : query.isError ? (
        <div className={styles.errorState}>
          <InlineError {...describeError(query.error, "Could not load the delivery history.")} />
          <Button
            size="sm"
            variant="secondary"
            leadingIcon="refresh"
            onClick={() => void query.refetch()}
          >
            Retry
          </Button>
        </div>
      ) : rows.length === 0 ? (
        chip === "all" ? (
          <div className={styles.empty}>{emptyState}</div>
        ) : (
          <p className={styles.emptyText}>{CHIP_EMPTY[chip]}</p>
        )
      ) : (
        // Paging keeps the previous page on screen; dimming says so, so a
        // stale page is never mistaken for the one that was just requested.
        <ol
          className={styles.rows}
          data-stale={query.isPlaceholderData || undefined}
          aria-busy={query.isPlaceholderData || undefined}
        >
          {rows.map((row, i) => (
            <DeliveryRow
              key={row.uid ?? `${row.date_epoch ?? 0}-${row.action ?? ""}-${i}`}
              row={row}
              variant={variant}
            />
          ))}
        </ol>
      )}

      {showControls ? (
        <div className={styles.controls}>
          <span>
            Page {page} / {pageCount} · {listTotal} total
          </span>
          <span className={styles.controlButtons}>
            {PAGE_SIZE_OPTIONS.map((n) => (
              <button
                key={n}
                type="button"
                className={styles.sizeChip}
                aria-pressed={pageSize === n}
                aria-label={`${n} per page`}
                data-active={pageSize === n || undefined}
                onClick={() => {
                  setPageSize(n);
                  setPage(1);
                }}
              >
                {n}
              </button>
            ))}
            <IconButton
              icon="chevron-left"
              label="Previous page"
              size="sm"
              variant="ghost"
              disabled={page <= 1}
              onClick={() => setPage((p) => Math.max(1, p - 1))}
            />
            <IconButton
              icon="chevron-right"
              label="Next page"
              size="sm"
              variant="ghost"
              disabled={page >= pageCount}
              onClick={() => setPage((p) => Math.min(pageCount, p + 1))}
            />
          </span>
        </div>
      ) : null}
    </div>
  );
}
