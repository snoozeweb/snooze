import { useEffect, useRef, useSyncExternalStore } from "react";
import type { IconName } from "@/shared/icons/icon-names";

/**
 * A command the currently-mounted page contributes to the ⌘K palette.
 *
 * The palette lives in the app shell and the context lives on the page, so
 * pages *publish* their commands into this module registry rather than the
 * palette reaching into page state. Everything the command needs is already
 * closed over in `run` — the palette only ever knows a label and a callback.
 */
export type PaletteAction = {
  /** Stable within a page; used as the React key and option id. */
  id: string;
  label: string;
  icon?: IconName;
  /** Right-aligned secondary text, e.g. "3 selected". */
  hint?: string;
  run: () => void;
};

type Entry = { list: PaletteAction[] };

let entries: Entry[] = [];
// Cached flattened view. useSyncExternalStore requires getSnapshot to return a
// referentially stable value between notifications, so this is recomputed only
// when the registry actually changes — never per call.
let snapshot: PaletteAction[] = [];
const listeners = new Set<() => void>();

function recompute(): void {
  snapshot = entries.flatMap((e) => e.list);
  for (const l of listeners) l();
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

function getSnapshot(): PaletteAction[] {
  return snapshot;
}

/**
 * Publishes `list` to the palette for as long as the calling component is
 * mounted. **Memoize `list`** — its identity is the effect's dependency, so an
 * inline array would re-register on every render.
 */
export function usePublishPaletteActions(list: PaletteAction[]): void {
  const entryRef = useRef<Entry | null>(null);
  useEffect(() => {
    const entry: Entry = { list };
    entryRef.current = entry;
    entries = [...entries, entry];
    recompute();
    return () => {
      entries = entries.filter((e) => e !== entry);
      recompute();
    };
  }, [list]);
}

/** Every published command, in mount order. Read by the command palette. */
export function usePaletteActions(): PaletteAction[] {
  return useSyncExternalStore(subscribe, getSnapshot, getSnapshot);
}
