import { createContext, useCallback, useContext, useEffect, useRef, useState } from "react";
import type { ReactNode } from "react";
import styles from "./LiveAnnouncer.module.css";

export type AnnounceOptions = {
  /**
   * Interrupt whatever the screen reader is saying. Reserve this for things
   * the operator must hear now (a cluster going down); routine refresh chatter
   * stays polite so it queues behind whatever they are reading.
   */
  assertive?: boolean;
};

export type AnnounceFn = (message: string, opts?: AnnounceOptions) => void;

/**
 * Several announcements landing inside this window collapse to the last one.
 * Polls arrive in bursts (the alerts list, the active-alert count and the
 * stats query can all settle within a few milliseconds of each other) and a
 * screen reader reading three sentences back to back is worse than silence.
 */
const COALESCE_MS = 300;

/**
 * A live region only speaks when its text *changes*. Two identical
 * announcements in a row (the count flips 5 → 6 → 5 → 6) would leave the DOM
 * node untouched the second time, so we blank the region and write the text
 * back a tick later — the standard "clear then set" trick. The gap only has to
 * outlast one paint.
 */
const REPAINT_MS = 50;

/**
 * Live regions are read on focus by some screen readers, so text left sitting
 * in one gets re-read minutes later, out of context. Wipe it once it has had
 * time to be spoken.
 */
const CLEAR_AFTER_MS = 5000;

type Politeness = "polite" | "assertive";

type TimerId = ReturnType<typeof setTimeout>;

/**
 * No-op default: a component that calls useAnnounce() outside the provider
 * (a unit test rendering it in isolation, a Storybook-style showroom page)
 * should stay silent, not crash. The provider is mounted once at the router
 * root, so the real app never takes this branch.
 */
const AnnouncerContext = createContext<AnnounceFn>(() => undefined);

/**
 * useAnnounce returns a stable function that speaks a sentence to screen-reader
 * users without moving focus or drawing anything on screen. Use it for changes
 * the operator did not ask for — a background poll swapping the rows under
 * them — where a sighted user gets the update for free and everyone else gets
 * nothing. Toasts already announce themselves (Radix Toast owns its own live
 * region), so never mirror a toast through here.
 */
export function useAnnounce(): AnnounceFn {
  return useContext(AnnouncerContext);
}

/**
 * LiveAnnouncerProvider mounts the app's two ARIA live regions — one polite,
 * one assertive — inside a single visually-hidden container, and hands
 * children the announce() function that drives them. Mounted once at the
 * router root so every route (and every hook under it) shares the same pair;
 * duplicating live regions per-page is how announcements end up spoken twice.
 */
export function LiveAnnouncerProvider({ children }: { children: ReactNode }) {
  const [polite, setPolite] = useState("");
  const [assertive, setAssertive] = useState("");

  // The message waiting out the coalesce window; overwritten (not queued) by
  // anything that lands before it flushes, so the last word wins.
  const pendingRef = useRef<{ message: string; politeness: Politeness } | null>(null);
  const coalesceRef = useRef<TimerId | null>(null);
  // Per-region timers: a polite announcement must not cancel the assertive
  // region's pending wipe, or stale text sits there until the next flip.
  const repaintRef = useRef<Record<Politeness, TimerId | null>>({ polite: null, assertive: null });
  const clearRef = useRef<Record<Politeness, TimerId | null>>({ polite: null, assertive: null });

  const announce = useCallback<AnnounceFn>((message, opts) => {
    // An empty announcement is indistinguishable from a cleared region — drop
    // it rather than blanking whatever is currently being read.
    if (!message) return;
    pendingRef.current = {
      message,
      politeness: opts?.assertive === true ? "assertive" : "polite",
    };
    if (coalesceRef.current !== null) clearTimeout(coalesceRef.current);
    coalesceRef.current = setTimeout(() => {
      coalesceRef.current = null;
      const next = pendingRef.current;
      pendingRef.current = null;
      if (!next) return;
      const { politeness } = next;
      const setText = politeness === "assertive" ? setAssertive : setPolite;
      setText("");
      if (repaintRef.current[politeness] !== null) {
        clearTimeout(repaintRef.current[politeness]);
      }
      repaintRef.current[politeness] = setTimeout(() => {
        repaintRef.current[politeness] = null;
        setText(next.message);
        if (clearRef.current[politeness] !== null) {
          clearTimeout(clearRef.current[politeness]);
        }
        clearRef.current[politeness] = setTimeout(() => {
          clearRef.current[politeness] = null;
          setText("");
        }, CLEAR_AFTER_MS);
      }, REPAINT_MS);
    }, COALESCE_MS);
  }, []);

  // Nothing here survives unmount: a timer firing into a torn-down tree is a
  // React warning at best and a leak at worst.
  useEffect(() => {
    const repaint = repaintRef.current;
    const clear = clearRef.current;
    const coalesce = coalesceRef;
    return () => {
      if (coalesce.current !== null) clearTimeout(coalesce.current);
      for (const t of [repaint.polite, repaint.assertive, clear.polite, clear.assertive]) {
        if (t !== null) clearTimeout(t);
      }
    };
  }, []);

  return (
    <AnnouncerContext.Provider value={announce}>
      {children}
      <div className={styles.visuallyHidden}>
        <div role="status" aria-live="polite" aria-atomic="true" data-testid="live-polite">
          {polite}
        </div>
        <div role="alert" aria-live="assertive" aria-atomic="true" data-testid="live-assertive">
          {assertive}
        </div>
      </div>
    </AnnouncerContext.Provider>
  );
}
