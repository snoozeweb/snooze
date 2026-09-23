// ConsoleConfig mirrors the Go ConsoleConfig struct served read-only at
// GET /api/v1/config (internal/api/routes_config.go). It carries org-wide
// web-console defaults: the alert-table columns, default filter, sort,
// auto-refresh interval, the severity rank ladder (+ its derived order), and
// light branding. There is deliberately no per-label colour map — a custom
// severity is placed by rank and inherits a theme-aware --severity-* token.
export type ConsoleConfig = {
  /** Ordered alert-table column ids. */
  columns: string[];
  /** Saved-search expression applied by default, or "". */
  default_filter: string;
  /** Default sort field; "-" prefix means descending (e.g. "-date_epoch"). */
  sort_by: string;
  /** Auto-refresh interval, in seconds. */
  refresh_interval: number;
  /** Label → rank map (0 = most severe); the Plan 21 ladder + operator additions. */
  severity_ranks: Record<string, number>;
  /** Labels most→least severe, derived from severity_ranks (never edited directly). */
  severity_order: string[];
  /** Logo URL or data: URI; "" uses the bundled logo. */
  logo: string;
  /** Browser/app title; "" uses the default. */
  title: string;
  /** "New alert" audio cue URL; "" disables it. */
  audio: string;
  /** Copy-to-clipboard template; "" uses the default. */
  clipboard_template: string;
};

// CONSOLE_FALLBACK is the offline fallback used when the /api/v1/config fetch
// has not resolved (or failed). It mirrors today's frontend hardcodes so the
// SPA renders identically with or without the server document. The
// severity_ranks/severity_order are intentionally empty here: severity-color's
// built-in RANK map remains the offline source of truth until the server map
// is applied via setSeverityRanks.
export const CONSOLE_FALLBACK: ConsoleConfig = {
  // Mirrors internal/api/routes_config.go `defaultColumns` — message-first,
  // with `process`/`source` defined in columns.tsx but off the default layout.
  columns: [
    "severity",
    "message",
    "hits",
    "state",
    "owner",
    "date_epoch",
    "host",
    "environment",
    "ttl",
  ],
  default_filter: "",
  sort_by: "-date_epoch",
  refresh_interval: 5,
  severity_ranks: {},
  severity_order: [],
  logo: "",
  title: "",
  audio: "",
  clipboard_template: "",
};
