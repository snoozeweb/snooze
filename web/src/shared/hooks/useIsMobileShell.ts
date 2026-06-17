import { useSyncExternalStore } from "react";

// Mirrors --bp-lg in tokens.css. Below this width the AppShell swaps from the
// desktop sidebar layout to the touch (bottom-bar + More-sheet) layout.
// 1023.98px so it never overlaps the desktop range at exactly 1024px.
const MOBILE_SHELL_QUERY = "(max-width: 1023.98px)";

function subscribe(onChange: () => void): () => void {
  if (typeof window === "undefined" || !window.matchMedia) return () => {};
  const mql = window.matchMedia(MOBILE_SHELL_QUERY);
  mql.addEventListener("change", onChange);
  return () => mql.removeEventListener("change", onChange);
}

function getSnapshot(): boolean {
  if (typeof window === "undefined" || !window.matchMedia) return false;
  return window.matchMedia(MOBILE_SHELL_QUERY).matches;
}

// Server / jsdom: assume the desktop shell so unit tests + the a11y audit
// render today's tree unchanged.
function getServerSnapshot(): boolean {
  return false;
}

export function useIsMobileShell(): boolean {
  return useSyncExternalStore(subscribe, getSnapshot, getServerSnapshot);
}
