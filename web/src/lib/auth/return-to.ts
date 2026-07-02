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
