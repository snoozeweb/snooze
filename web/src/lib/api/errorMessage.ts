import { ApiError } from "./client";

/**
 * Human copy for a failed request: a short, sentence-case `summary` fit for a
 * toast or an inline banner, plus an optional `secondary` line carrying the
 * server's raw detail — kept verbatim so it can be pasted into a ticket.
 *
 * The product's failure moments (a failed ack, a bad login, a rejected save)
 * used to surface the backend's own vocabulary verbatim — lowercase fragments
 * like `internal server error` or `invalid credentials`. This is the one
 * place that translates those into the app's voice; every mutation error
 * surface should route through it instead of printing `err.message`/`detail`
 * directly.
 */
export type ErrorCopy = {
  summary: string;
  secondary?: string;
};

function sentenceCase(s: string): string {
  const t = s.trim();
  if (!t) return t;
  const cased = t[0]!.toUpperCase() + t.slice(1);
  return /[.!?]$/.test(cased) ? cased : `${cased}.`;
}

/**
 * Describe an unknown error (typically caught from a mutation) in the
 * product's voice.
 *
 * - 401 → "Wrong username or password." (the API's `invalid credentials`
 *   detail is the login-only 401 case; other 401s are already caught by the
 *   session's unauthorized handler before a caller ever sees them).
 * - Network failures (fetch rejects with a TypeError before a Response
 *   exists) → a connectivity message, no server detail to attach.
 * - 5xx → a generic "the server ran into a problem" summary, with the raw
 *   detail preserved as `secondary` for a bug report.
 * - Other 4xx → the server's own detail, sentence-cased, as the summary.
 */
export function describeError(err: unknown, fallback = "Something went wrong."): ErrorCopy {
  if (err instanceof ApiError) {
    if (err.status === 401) {
      return { summary: "Wrong username or password." };
    }
    if (err.status >= 500 || err.code === "internal") {
      return { summary: "The server ran into a problem.", secondary: err.detail };
    }
    const summary = err.detail ? sentenceCase(err.detail) : fallback;
    return { summary };
  }
  if (err instanceof TypeError) {
    // fetch() rejects with a TypeError ("Failed to fetch"/"NetworkError…")
    // when the request never reached a server — no useful detail to show.
    return { summary: "Couldn't reach the server. Check your connection and try again." };
  }
  return { summary: fallback };
}

/**
 * Describe a failed action against a named subject, for inline dialog/toast
 * copy: "Couldn't acknowledge srv-prod-db-01 — the server ran into a
 * problem." `subject` should already be human ("srv-prod-db-01", "3 alerts").
 */
export function describeActionError(verb: string, subject: string, err: unknown): ErrorCopy {
  const { summary, secondary } = describeError(err);
  // Strip the trailing period so it reads as one sentence when joined.
  const clause = summary.endsWith(".") ? summary.slice(0, -1) : summary;
  return {
    summary: `Couldn't ${verb} ${subject} — ${clause[0]!.toLowerCase()}${clause.slice(1)}.`,
    ...(secondary ? { secondary } : {}),
  };
}
