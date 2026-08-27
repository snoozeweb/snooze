/**
 * isSafeInternalPath — true when `p` is a same-origin path safe to hand to the
 * router / SSO server as a post-login destination: it must start with a single
 * "/" and not "//" or "/\" (both of which browsers treat as protocol-relative
 * URLs to another host → open-redirect). Values arriving from URLSearchParams /
 * the router are already decoded, so callers must NOT decodeURIComponent again
 * (that mangles encoded query state and throws on a raw "%").
 */
export function isSafeInternalPath(p: string | null | undefined): p is string {
  return typeof p === "string" && /^\/(?![/\\])/.test(p);
}

/** The login route's path, kept here so the redirect helpers below and their
 *  callers cannot drift apart. */
export const LOGIN_PATH = "/web/login";

/**
 * loginRedirectSearch builds the search object for a bounce to the login page,
 * carrying the destination to return to after signing back in.
 *
 * `href` may be a full URL or a path; either way the value stored is a plain
 * same-origin path. It is NOT pre-encoded: the router encodes search values on
 * the way out and decodes them on the way in, so encoding here would leave the
 * login page holding "%2Fweb%2Falerts" — which fails isSafeInternalPath and
 * silently drops the destination. A login page URL is never captured, since
 * that would return the user to the login page after logging in.
 */
export function loginRedirectSearch(href: string | null | undefined): { return_to?: string } {
  if (typeof href !== "string" || href === "") return {};
  let path = href;
  if (/^https?:\/\//i.test(path)) {
    try {
      const u = new URL(path);
      path = `${u.pathname}${u.search}`;
    } catch {
      return {};
    }
  }
  if (!isSafeInternalPath(path)) return {};
  if (path === LOGIN_PATH || path.startsWith(`${LOGIN_PATH}?`) || path.startsWith(`${LOGIN_PATH}/`))
    return {};
  return { return_to: path };
}

/**
 * normalizeReturnTo accepts a `return_to` as it arrives from the router or from
 * URLSearchParams and yields a safe same-origin path, or null.
 *
 * Values are expected already decoded. As a courtesy it also accepts a value
 * that is still percent-encoded ("%2Fweb%2Falerts") — links minted by older
 * builds double-encoded — decoding exactly once more in that case.
 */
export function normalizeReturnTo(raw: string | null | undefined): string | null {
  if (typeof raw !== "string" || raw === "") return null;
  if (isSafeInternalPath(raw)) return raw;
  if (/^%2[Ff]/.test(raw)) {
    try {
      const once = decodeURIComponent(raw);
      if (isSafeInternalPath(once)) return once;
    } catch {
      return null;
    }
  }
  return null;
}
