import { useCallback, useEffect, useState } from "react";

export type Theme = "dark" | "light";

const THEME_KEY = "snooze.theme";
const DEFAULT_THEME: Theme = "dark";

// Browser-chrome color per theme. Literals mirror --bg-canvas in
// src/styles/theme.{dark,light}.css (a <meta> tag can't read CSS vars).
// The initial value is set in index.html; this keeps it in sync on toggle.
const THEME_COLOR: Record<Theme, string> = {
  dark: "#0a0c0e",
  light: "#f4f5f7",
};

function readDomTheme(): Theme {
  const value = document.documentElement.getAttribute("data-theme");
  return value === "light" || value === "dark" ? value : DEFAULT_THEME;
}

function applyThemeColorMeta(theme: Theme): void {
  const meta = document.querySelector('meta[name="theme-color"]');
  if (meta) meta.setAttribute("content", THEME_COLOR[theme]);
}

function applyDomTheme(theme: Theme): void {
  document.documentElement.setAttribute("data-theme", theme);
  applyThemeColorMeta(theme);
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
      applyThemeColorMeta(next);
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
