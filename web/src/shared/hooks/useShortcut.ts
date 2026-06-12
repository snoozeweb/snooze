import { useEffect } from "react";

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

export function useShortcut(
  combo: string,
  handler: (event: KeyboardEvent) => void,
  options: ShortcutOptions = {},
): void {
  useEffect(() => {
    const parsed = parseCombo(combo);
    function onKeyDown(e: KeyboardEvent) {
      if (!options.enableInInputs && isEditable(e.target)) return;
      const modPressed = IS_MAC ? e.metaKey : e.ctrlKey;
      if (parsed.ctrlOrCmd && !modPressed) return;
      if (!parsed.ctrlOrCmd && modPressed) return;
      if (parsed.shift !== e.shiftKey) return;
      if (parsed.alt !== e.altKey) return;
      if (e.key.toLowerCase() !== parsed.key) return;
      e.preventDefault();
      handler(e);
    }
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [combo, handler, options.enableInInputs]);
}
