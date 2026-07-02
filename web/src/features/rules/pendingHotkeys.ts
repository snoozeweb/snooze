// Decides whether a global keydown should drive the rules tree's pending-reorder
// hotkeys (Escape = cancel, Enter = validate). It must NOT fire when the user is
// typing in a field, nor when a modal is open: opening the rule editor drawer
// while a reorder is staged and pressing Escape to close it would otherwise also
// cancel the reorder — silently discarding the staged moves the useBlocker guard
// exists to protect.
export function shouldFirePendingHotkey(target: EventTarget | null): boolean {
  const el = target as HTMLElement | null;
  if (el && typeof el.tagName === "string") {
    const tag = el.tagName;
    if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT" || el.isContentEditable) {
      return false;
    }
  }
  // A modal (editor drawer / confirm dialog) owns Escape/Enter while it is open.
  if (
    typeof document !== "undefined" &&
    document.querySelector('[role="dialog"], [role="alertdialog"]')
  ) {
    return false;
  }
  return true;
}
