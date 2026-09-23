import { Fragment, useCallback, useRef } from "react";
import type { KeyboardEvent, ReactNode } from "react";
import { IconButton } from "./IconButton";
import { isEditable } from "@/shared/hooks/useShortcut";
import { Drawer, DrawerBody, DrawerContent, DrawerTitle } from "./Drawer";
import styles from "./RowDetailsDrawer.module.css";

export type RowDetailsDrawerProps<T> = {
  rows: T[];
  rowKey: (row: T) => string;
  /** Key of the open row; null/absent-in-rows → renders nothing. */
  activeKey: string | null;
  /** Retarget to rows[index] (prev/next buttons, in-drawer ArrowUp/ArrowDown). */
  onNavigate: (index: number) => void;
  onClose: () => void;
  renderDetails: (row: T) => ReactNode;
  detailsTitle?: ((row: T) => ReactNode) | undefined;
  /** Toolbar IconButtons for the active row (rendered before the counter). */
  actions?: ReactNode;
};

/** Detail side panel shared by every table-like surface (DataTable,
 *  RulesTreeTable): a wide Drawer showing one row's details with prev/next
 *  navigation and an "N / M" position counter. Extracted from DataTable so
 *  bespoke row surfaces (the Rules tree, which isn't a <table>) can offer the
 *  same drawer without depending on DataTable itself. */
export function RowDetailsDrawer<T>({
  rows,
  rowKey,
  activeKey,
  onNavigate,
  onClose,
  renderDetails,
  detailsTitle,
  actions,
}: RowDetailsDrawerProps<T>) {
  // The detail drawer's key-nav wrapper — receives the drawer's initial focus
  // (see onOpenAutoFocus on its DrawerContent).
  const detailKeyNavRef = useRef<HTMLDivElement>(null);

  // In-drawer ArrowUp/ArrowDown — and their j/k aliases — page to the previous
  // / next row, so the traversal keys an operator just used in the grid keep
  // working once the drawer is open. Guarded by isEditable so typing in the
  // comment composer never navigates, and by the modifier check so Ctrl+K
  // still reaches the command palette. Attached to a wrapper inside
  // DrawerContent so it fires wherever focus sits in the drawer.
  const onDrawerKeyDown = useCallback(
    (e: KeyboardEvent<HTMLDivElement>) => {
      const bare = !e.ctrlKey && !e.metaKey && !e.altKey;
      const up = e.key === "ArrowUp" || (bare && e.key.toLowerCase() === "k");
      const down = e.key === "ArrowDown" || (bare && e.key.toLowerCase() === "j");
      if (!up && !down) return;
      if (isEditable(e.target)) return;
      if (activeKey === null) return;
      const idx = rows.findIndex((r) => rowKey(r) === activeKey);
      if (idx < 0) return;
      if (up) {
        if (idx > 0) {
          e.preventDefault();
          onNavigate(idx - 1);
        }
      } else if (idx < rows.length - 1) {
        e.preventDefault();
        onNavigate(idx + 1);
      }
    },
    [activeKey, rows, rowKey, onNavigate],
  );

  const index = activeKey != null ? rows.findIndex((r) => rowKey(r) === activeKey) : -1;
  const resolved = index >= 0 ? rows[index] : undefined;

  // `rows` is re-derived on every poll, page and filter change, so the open row
  // can vanish from under an operator who is mid-sentence in the comment
  // composer or the analysis editor. Re-deriving `row` from `rows` alone would
  // then render nothing and tear the whole subtree down, losing the draft with
  // no prompt. Keep the last row this activeKey resolved to and go on showing
  // it: the drawer stays on the row it was opened on until the key itself
  // changes (a retarget) or is cleared (a close). Assigning during render is
  // safe — the value is a pure function of the props of this same render.
  const lastRowRef = useRef<{ key: string; row: T } | null>(null);
  if (activeKey === null) lastRowRef.current = null;
  else if (resolved !== undefined) lastRowRef.current = { key: activeKey, row: resolved };
  const snapshot =
    activeKey !== null && lastRowRef.current?.key === activeKey
      ? lastRowRef.current.row
      : undefined;
  const row = resolved ?? snapshot;

  if (!row) return null;
  // Off-page: the row is still on screen but its position in the list is not a
  // fact any more, so the counter says so and prev/next go inert rather than
  // paging from a bogus index.
  const onPage = index >= 0;

  return (
    <Drawer
      // Non-modal: this is a side panel, not a dialog. It opens beside the
      // table it belongs to and the operator keeps working there — paging,
      // sorting, opening the next row. Modal Radix put a fixed scrim at
      // --z-modal over the whole app, so every one of those controls was inert
      // while the panel was open: "next page" simply did nothing.
      modal={false}
      open
      onOpenChange={(o) => {
        if (!o) onClose();
      }}
    >
      <DrawerContent
        wide
        nonModal
        // …and it does NOT close when the operator touches the table. Paging
        // to another page with the panel open is a normal thing to do; having
        // the panel vanish on that click is the same bug wearing a different
        // coat. Escape and the ✕ close it; a row click retargets it.
        onPointerDownOutside={(e) => e.preventDefault()}
        onInteractOutside={(e) => e.preventDefault()}
        // Radix would focus the first tabbable element on open — a
        // quick-action IconButton here, which pops its Tooltip over the
        // drawer. Land the initial focus on the key-nav wrapper instead:
        // neutral (no tooltip), and ArrowUp/ArrowDown paging works
        // immediately without an extra Tab.
        onOpenAutoFocus={(e) => {
          e.preventDefault();
          detailKeyNavRef.current?.focus();
        }}
      >
        {/* Flex-column wrapper filling the drawer: gives ArrowUp/ArrowDown
            a single keydown target covering the whole drawer (title
            toolbar + body) and receives the initial open focus. It's a
            passive event-delegation container — the interactive controls
            (nav buttons, comment composer) live inside and own their own
            semantics — so it carries no role of its own. */}
        {/* eslint-disable-next-line jsx-a11y/no-static-element-interactions */}
        <div
          ref={detailKeyNavRef}
          tabIndex={-1}
          className={styles.detailKeyNav}
          onKeyDown={onDrawerKeyDown}
        >
          <DrawerTitle
            onClose={onClose}
            toolbar={
              <>
                {actions}
                <span className={styles.detailsPosition}>
                  {onPage ? index + 1 : "—"} / {rows.length}
                </span>
                {/* The key is named in the label so the drawer teaches its own
                    shortcut — hovering the button is how most operators will
                    find out that K / J page through the list from here. */}
                <IconButton
                  icon="chevron-up"
                  label="Previous row (K)"
                  size="sm"
                  disabled={!onPage || index <= 0}
                  onClick={() => onNavigate(index - 1)}
                />
                <IconButton
                  icon="chevron-down"
                  label="Next row (J)"
                  size="sm"
                  disabled={!onPage || index >= rows.length - 1}
                  onClick={() => onNavigate(index + 1)}
                />
              </>
            }
          >
            {detailsTitle?.(row) ?? "Details"}
          </DrawerTitle>
          {/* Keyed by the row: a retarget (prev/next, J/K, another row) is a
              different subject, not new props for the same one. Without the
              key React reconciles the two renders into one instance, so a
              details subtree holding state — the inspector's open tab, an
              editor's form defaults captured at mount — carries the previous
              row's draft onto the new row, and a save addressed by the fresh
              uid writes it to the wrong record. */}
          <DrawerBody>
            <Fragment key={rowKey(row)}>{renderDetails(row)}</Fragment>
          </DrawerBody>
        </div>
      </DrawerContent>
    </Drawer>
  );
}
