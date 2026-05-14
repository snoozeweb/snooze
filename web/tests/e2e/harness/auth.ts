// web/tests/e2e/harness/auth.ts
import type { Page } from "@playwright/test";

export type LoginShortcutOptions = {
  baseURL: string;
  /** A real JWT token returned by the server (e.g. from mintRootToken). */
  token: string;
};

/**
 * Skips the login page by writing the auth store directly into localStorage.
 *
 * Mirrors exactly what `web/src/lib/auth/storage.ts` writes after a real login:
 *   - KEY "snooze-token"  → the raw JWT string
 *   - KEY "snooze-claims" → JSON of the decoded JWT payload (JwtClaims)
 *
 * The claims are decoded inside the page context (no npm dependency needed)
 * by base64-decoding the JWT's second segment. The store's `readStorageSnapshot`
 * will find both keys and consider the session authenticated.
 */
export async function loginAsAdmin(page: Page, opts: LoginShortcutOptions): Promise<void> {
  await page.addInitScript((token) => {
    // TOKEN_KEY = "snooze-token", CLAIMS_KEY = "snooze-claims"
    window.localStorage.setItem("snooze-token", token);

    // Decode the JWT payload (second base64url segment) so the store's
    // readClaims() sees a valid JwtClaims object without jwt-decode.
    try {
      const payload = token.split(".")[1];
      if (payload) {
        // base64url → base64 → JSON
        const b64 = payload.replace(/-/g, "+").replace(/_/g, "/");
        const json = decodeURIComponent(
          atob(b64)
            .split("")
            .map((c) => "%" + c.charCodeAt(0).toString(16).padStart(2, "0"))
            .join(""),
        );
        window.localStorage.setItem("snooze-claims", json);
      }
    } catch {
      // If decoding fails the store falls back to decoding the token itself.
    }
  }, opts.token);

  await page.goto(opts.baseURL + "/web/");
}
