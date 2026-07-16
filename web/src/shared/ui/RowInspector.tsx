import { useCallback, useEffect, useRef, useState } from "react";
import type { PointerEvent as ReactPointerEvent, KeyboardEvent as ReactKeyboardEvent } from "react";
import type { ReactNode } from "react";
import { createPortal } from "react-dom";
import { IconButton } from "./IconButton";
import { isEditable } from "@/shared/hooks/useShortcut";
import styles from "./RowInspector.module.css";

export type RowInspectorProps = {
  /** Header title — truncates, takes the flex-1 slot. */
  title: ReactNode;
  /** Quick-action IconButtons cluster, rendered before the nav controls. */
  actions?: ReactNode;
  /** Renders a mono "N / M" position counter. `index` is 0-based. */
  position?: { index: number; total: number };
  /** Previous-row handler. `undefined` disables the button (top of list). */
  onPrev?: (() => void) | undefined;
  /** Next-row handler. `undefined` disables the button (bottom of list). */
  onNext?: (() => void) | undefined;
  onClose: () => void;
  children: ReactNode;
};

const STORAGE_KEY = "snooze.rowInspector.width";
const DEFAULT_WIDTH = 600;
const MIN_WIDTH = 420;
const MAX_WIDTH = 900;
// Keep at least this much table visible to the left of the panel.
const VIEWPORT_MARGIN = 240;
// Keyboard resize step for the ArrowLeft/ArrowRight handle nudge.
const RESIZE_STEP = 32;

/** Clamp a requested width to [420, min(900, viewport - 240)]. */
function clampWidth(w: number): number {
  const viewport = typeof window !== "undefined" ? window.innerWidth : 1280;
  const max = Math.min(MAX_WIDTH, viewport - VIEWPORT_MARGIN);
  // On very narrow viewports the computed max can dip below MIN_WIDTH; keep the
  // lower bound sane so the clamp never inverts.
  const lo = Math.min(MIN_WIDTH, max);
  return Math.max(lo, Math.min(max, w));
}

/** Read the persisted width once (lazy state initializer). */
function readStoredWidth(): number {
  if (typeof window === "undefined") return DEFAULT_WIDTH;
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY);
    if (raw !== null) {
      const n = Number(raw);
      if (Number.isFinite(n) && n > 0) return clampWidth(n);
    }
  } catch {
    // localStorage can throw (privacy mode, disabled storage) — fall back.
  }
  return DEFAULT_WIDTH;
}

function persistWidth(w: number): void {
  try {
    window.localStorage.setItem(STORAGE_KEY, String(Math.round(w)));
  } catch {
    // Ignore write failures — width persistence is best-effort.
  }
}

/**
 * RowInspector — a presentational, non-modal panel docked to the right viewport
 * edge, rendered through a portal to `document.body`. There is no backdrop and
 * no focus trap: the list behind it stays fully interactive, matching the
 * Datadog/Sentry-style triage inspector. The visual grammar mirrors `Drawer`
 * (surface bg, 1px border-left, shadow-pop, amber keyline under the header,
 * slide-in on mount).
 *
 * It owns only its own resize width (persisted to localStorage); all
 * navigation/close behaviour is delegated to the parent via the handler props.
 */
export function RowInspector({
  title,
  actions,
  position,
  onPrev,
  onNext,
  onClose,
  children,
}: RowInspectorProps) {
  const [width, setWidth] = useState<number>(() => readStoredWidth());
  // Refs so the pointer-move / window-keydown handlers read the latest values
  // without re-subscribing.
  const widthRef = useRef(width);
  widthRef.current = width;
  const onCloseRef = useRef(onClose);
  onCloseRef.current = onClose;

  // Escape closes the inspector. Ignored when the event was already handled
  // (e.defaultPrevented) or fired from an editable element — the same guard the
  // per-row keyboard shortcuts use.
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key !== "Escape") return;
      if (e.defaultPrevented) return;
      if (isEditable(e.target)) return;
      e.preventDefault();
      onCloseRef.current();
    }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  // Left-edge drag resize. Pointer capture keeps the drag alive when the
  // pointer leaves the 5px handle; the width is persisted on release only.
  const onHandlePointerDown = useCallback((e: ReactPointerEvent<HTMLDivElement>) => {
    e.preventDefault();
    const handle = e.currentTarget;
    const pointerId = e.pointerId;
    const startX = e.clientX;
    const startWidth = widthRef.current;
    try {
      handle.setPointerCapture?.(pointerId);
    } catch {
      // setPointerCapture is unavailable in some test environments — the
      // move/up listeners below still work without capture.
    }
    const onMove = (ev: PointerEvent) => {
      // The panel is docked right, so dragging the handle LEFT widens it.
      const delta = startX - ev.clientX;
      setWidth(clampWidth(startWidth + delta));
    };
    const onUp = () => {
      handle.removeEventListener("pointermove", onMove);
      handle.removeEventListener("pointerup", onUp);
      try {
        handle.releasePointerCapture?.(pointerId);
      } catch {
        // ignore
      }
      persistWidth(widthRef.current);
    };
    handle.addEventListener("pointermove", onMove);
    handle.addEventListener("pointerup", onUp);
  }, []);

  const onHandleKeyDown = useCallback((e: ReactKeyboardEvent<HTMLDivElement>) => {
    if (e.key === "ArrowLeft") {
      // Grow (docked right → left is bigger).
      e.preventDefault();
      setWidth((w) => {
        const next = clampWidth(w + RESIZE_STEP);
        persistWidth(next);
        return next;
      });
    } else if (e.key === "ArrowRight") {
      e.preventDefault();
      setWidth((w) => {
        const next = clampWidth(w - RESIZE_STEP);
        persistWidth(next);
        return next;
      });
    }
  }, []);

  const onHandleDoubleClick = useCallback(() => {
    const next = clampWidth(DEFAULT_WIDTH);
    setWidth(next);
    persistWidth(next);
  }, []);

  const panel = (
    // <aside> already carries the implicit "complementary" role.
    <aside aria-label="Row inspector" className={styles.panel} style={{ width: `${width}px` }}>
      {/* eslint-disable jsx-a11y/no-noninteractive-element-interactions, jsx-a11y/no-noninteractive-tabindex --
          A focusable WAI-ARIA window-splitter: interactive by design (drag to
          resize, ArrowLeft/ArrowRight to nudge, double-click to reset), but
          jsx-a11y treats the separator role as non-interactive. */}
      <div
        className={styles.resizeHandle}
        role="separator"
        aria-orientation="vertical"
        aria-label="Resize inspector"
        aria-valuenow={Math.round(width)}
        tabIndex={0}
        onPointerDown={onHandlePointerDown}
        onKeyDown={onHandleKeyDown}
        onDoubleClick={onHandleDoubleClick}
      />
      {/* eslint-enable jsx-a11y/no-noninteractive-element-interactions, jsx-a11y/no-noninteractive-tabindex */}
      <header className={styles.header}>
        <div className={styles.title}>{title}</div>
        {actions !== undefined ? <div className={styles.actions}>{actions}</div> : null}
        {position ? (
          <span className={styles.position}>
            {position.index + 1} / {position.total}
          </span>
        ) : null}
        <div className={styles.nav}>
          <IconButton
            icon="chevron-up"
            label="Previous row"
            size="sm"
            disabled={!onPrev}
            {...(onPrev ? { onClick: onPrev } : {})}
          />
          <IconButton
            icon="chevron-down"
            label="Next row"
            size="sm"
            disabled={!onNext}
            {...(onNext ? { onClick: onNext } : {})}
          />
          <IconButton icon="x" label="Close inspector" size="sm" onClick={onClose} />
        </div>
      </header>
      <div className={styles.body}>{children}</div>
    </aside>
  );

  return createPortal(panel, document.body);
}
