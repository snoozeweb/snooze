// The alerts route's search-param contract, kept apart from router.tsx so a
// test can build a route with the real validator without importing the
// router module (which arms the background token refresh on load).

export type AlertsSearchParams = {
  state?: string;
  severity?: string;
  environment?: string;
  search?: string;
  page?: number;
  orderby?: string;
  asc?: boolean;
  uid?: string;
  // Lifecycle tab id and comma-separated environment UIDs. AlertsPage drives
  // its filter state off these (and the dashboard drill-downs deep-link to
  // them), so validateSearch must preserve them — otherwise they'd be
  // stripped on every navigation through this route.
  tab?: string;
  env?: string;
  // Open detail-drawer record key. AlertsPage syncs the modal detail drawer's
  // open alert here so it's shareable / deep-linkable.
  record?: string;
  // Rides with `record`: the inspector tab on screen. Absent means Timeline,
  // the default, so only the other four are ever written. The legacy
  // `analysis=1` deep link (older dashboard links, bookmarks) still parses and
  // comes back as `pane=analysis`.
  pane?: AlertPane;
};

/** The inspector tabs a URL can name; Timeline is the absence of `pane`. */
export type AlertPane = "flow" | "analysis" | "deliveries" | "record";
const ALERT_PANES: readonly string[] = ["flow", "analysis", "deliveries", "record"];

/**
 * Exported for its own test: this is the one validator with real rules in it
 * (two spellings of a boolean, a key that is rejected when empty), and a route
 * definition is not reachable from a test.
 */
export function validateAlertsSearch(raw: Record<string, unknown>): AlertsSearchParams {
  {
    const out: Record<string, unknown> = {};
    const s = (k: string) => (typeof raw[k] === "string" ? raw[k] : undefined);
    const n = (k: string) => {
      const v = raw[k];
      if (typeof v === "number") return v;
      if (typeof v === "string" && /^\d+$/.test(v)) return Number(v);
      return undefined;
    };
    const b = (k: string) => {
      const v = raw[k];
      if (typeof v === "boolean") return v;
      if (v === "true") return true;
      if (v === "false") return false;
      return undefined;
    };
    const setIf = (k: string, v: unknown) => {
      if (v !== undefined) out[k] = v;
    };
    setIf("state", s("state"));
    setIf("severity", s("severity"));
    setIf("environment", s("environment"));
    setIf("search", s("search"));
    setIf("page", n("page"));
    setIf("orderby", s("orderby"));
    setIf("asc", b("asc"));
    setIf("uid", s("uid"));
    setIf("tab", s("tab"));
    setIf("env", s("env"));
    // An empty `?record=` is not "open nothing", it's a key that matches no
    // row — the same reason the notifications route refuses an empty
    // `?details=`. Reject it the way an absent param is rejected.
    const recordKey = s("record");
    if (recordKey !== undefined && recordKey !== "") out["record"] = recordKey;
    // An unknown pane — and `timeline`, which is the absence of the param —
    // is dropped, so the URL only ever carries one spelling of each state.
    const pane = s("pane");
    if (pane !== undefined && ALERT_PANES.includes(pane)) {
      out["pane"] = pane;
    } else if (b("analysis") === true || raw["analysis"] === 1 || raw["analysis"] === "1") {
      // The legacy spelling. Only its truthy forms mean anything: a deep link
      // is as likely to be typed as clicked, hence the `1` forms beside `b()`'s
      // `true`. It is not kept — the next navigation writes `pane` instead.
      out["pane"] = "analysis";
    }
    return out as AlertsSearchParams;
  }
}
