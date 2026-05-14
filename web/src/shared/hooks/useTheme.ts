import { useCallback, useEffect, useState } from "react";

export type Theme = "dark" | "light";

const THEME_KEY = "snooze.theme";
const DEFAULT_THEME: Theme = "dark";

function readDomTheme(): Theme {
  const value = document.documentElement.getAttribute("data-theme");
  return value === "light" || value === "dark" ? value : DEFAULT_THEME;
}

function applyDomTheme(theme: Theme): void {
  document.documentElement.setAttribute("data-theme", theme);
}

function persistTheme(theme: Theme): void {
  try {
    localStorage.setItem(THEME_KEY, theme);
  } catch {
    // localStorage can throw on private-mode Safari, disk-full, etc.
    // Theme falls back to the next page load's inline script.
  }
}

/**
 * useTheme exposes the current theme and a setter. The hook is the
 * authoritative bridge between React state and the html element's
 * data-theme attribute (initialised by the inline script in index.html).
 */
export function useTheme(): {
  theme: Theme;
  setTheme: (next: Theme) => void;
  toggleTheme: () => void;
} {
  const [theme, setThemeState] = useState<Theme>(() => readDomTheme());

  // Keep React state in sync if a *different* component or external
  // code mutated data-theme after we initialised.
  useEffect(() => {
    const observer = new MutationObserver(() => {
      const next = readDomTheme();
      setThemeState((prev) => (prev === next ? prev : next));
    });
    observer.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ["data-theme"],
    });
    return () => observer.disconnect();
  }, []);

  const setTheme = useCallback((next: Theme) => {
    applyDomTheme(next);
    persistTheme(next);
    setThemeState(next);
  }, []);

  const toggleTheme = useCallback(() => {
    const next: Theme = readDomTheme() === "dark" ? "light" : "dark";
    applyDomTheme(next);
    persistTheme(next);
    setThemeState(next);
  }, []);

  return { theme, setTheme, toggleTheme };
}
