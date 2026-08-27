import { readRefreshToken } from "./storage";

/**
 * revokeStoredRefreshToken tells the server to invalidate the refresh token
 * this browser holds, then returns. Clearing localStorage alone does not end a
 * session: the refresh token stays valid server-side for the whole
 * refresh_token_lease (7 days by default), so anyone who captured it — a shared
 * machine, a synced browser profile, a backup — can still mint access tokens
 * long after the operator pressed "Log out".
 *
 * Best-effort by design. The endpoint always answers 204 (an unknown or
 * already-revoked token is not an error), and a failure here must never block
 * or reverse the local sign-out: the user asked to be logged out, so they are,
 * whatever the network says.
 *
 * `keepalive` matters more than it looks — sign-out navigates away immediately,
 * and without it the browser is free to cancel the in-flight request as the
 * page tears down, which is exactly the case this function exists for.
 *
 * Lives in its own leaf module (no store, no api client) so the auth store can
 * call it without an import cycle.
 */
export function revokeStoredRefreshToken(): void {
  let token: string | null = null;
  try {
    token = readRefreshToken();
  } catch {
    return;
  }
  if (!token) return;
  if (typeof fetch !== "function") return;
  try {
    void fetch("/api/v1/login/logout", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ refresh_token: token }),
      keepalive: true,
    }).catch(() => {
      // Offline, tab closing, server down — the local session is gone either
      // way and the token expires on its own lease.
    });
  } catch {
    // Some environments throw synchronously (no network stack in a test env).
  }
}
