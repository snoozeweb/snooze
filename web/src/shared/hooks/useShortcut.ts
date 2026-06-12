import { useEffect, useRef } from "react";

export type ShortcutOptions = {
  enableInInputs?: boolean;
};

const IS_MAC = typeof navigator !== "undefined" && /mac/i.test(navigator.platform);

function parseCombo(combo: string): {
  key: string;
  ctrlOrCmd: boolean;
  shift: boolean;
  alt: boolean;
} {
  const parts = combo
    .toLowerCase()
    .split("+")
    .map((p) => p.trim());
  return {
    key: parts[parts.length - 1] ?? "",
    ctrlOrCmd: parts.includes("mod") || parts.includes("ctrl") || parts.includes("cmd"),
    shift: parts.includes("shift"),
    alt: parts.includes("alt") || parts.includes("opt"),
  };
}

/**
 * isEditable reports whether an event target is a text-editing surface
 * (input/textarea/select/contenteditable). Exported so other keyboard
 * handlers — e.g. DataTable's row key bindings — can apply the same guard
 * and not steal keystrokes the user is typing into a field.
 */
export function isEditable(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  const tag = target.tagName;
  if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT") return true;
  if (target.isContentEditable) return true;
  return false;
}

// ---------------------------------------------------------------------------
// Module-level registry: one window keydown listener for all useShortcut
// instances. Each binding stores a stable ref to its latest handler so
// re-renders with fresh inline arrows don't cause re-subscriptions.
// ---------------------------------------------------------------------------

type Binding = {
  parsed: ReturnType<typeof parseCombo>;
  enableInInputs: boolean;
  // Latest-ref pattern: the ref object itself is stable; its .current is
  // updated every render without touching the registry.
  handlerRef: { current: (e: KeyboardEvent) => void };
};

// Registry key → Set<Binding>. Multiple bindings for the same combo are
// allowed (different components can bind the same combo independently).
const registry = new Map<string, Set<Binding>>();

function globalKeyDown(e: KeyboardEvent): void {
  for (const [, bindings] of registry) {
    for (const binding of bindings) {
      if (!binding.enableInInputs && isEditable(e.target)) continue;
      const parsed = binding.parsed;
      const modPressed = IS_MAC ? e.metaKey : e.ctrlKey;
      if (parsed.ctrlOrCmd && !modPressed) continue;
      if (!parsed.ctrlOrCmd && modPressed) continue;
      if (parsed.shift !== e.shiftKey) continue;
      if (parsed.alt !== e.altKey) continue;
      if (e.key.toLowerCase() !== parsed.key) continue;
      e.preventDefault();
      binding.handlerRef.current(e);
    }
  }
}

function addBinding(combo: string, binding: Binding): void {
  let set = registry.get(combo);
  if (!set) {
    set = new Set();
    registry.set(combo, set);
  }
  set.add(binding);
  // Attach the shared listener when the first binding is registered.
  if (registry.size === 1 && set.size === 1) {
    window.addEventListener("keydown", globalKeyDown);
  }
}

function removeBinding(combo: string, binding: Binding): void {
  const set = registry.get(combo);
  if (!set) return;
  set.delete(binding);
  if (set.size === 0) registry.delete(combo);
  // Detach when no bindings remain at all.
  if (registry.size === 0) {
    window.removeEventListener("keydown", globalKeyDown);
  }
}

// ---------------------------------------------------------------------------

export function useShortcut(
  combo: string,
  handler: (event: KeyboardEvent) => void,
  options: ShortcutOptions = {},
): void {
  // A stable ref that always points to the latest handler. The registry holds
  // the ref object; updating .current here is invisible to the registry but
  // the global listener always calls the latest handler.
  const handlerRef = useRef(handler);
  // Always keep it current so callers using inline arrows get the latest
  // closure without re-registering.
  handlerRef.current = handler;

  const enableInInputs = options.enableInInputs ?? false;

  useEffect(() => {
    const parsed = parseCombo(combo);
    const binding: Binding = { parsed, enableInInputs, handlerRef };
    addBinding(combo, binding);
    return () => {
      removeBinding(combo, binding);
    };
    // combo and enableInInputs are the structural keys; if either changes we
    // must re-register. handlerRef is a stable object whose .current we update
    // above, so it never forces a re-registration.
  }, [combo, enableInInputs]);
}
