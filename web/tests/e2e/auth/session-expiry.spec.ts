// web/tests/e2e/auth/session-expiry.spec.ts
//
// What happens when the access token stops being good while the operator is
// still working. Two guarantees, in order of preference:
//
//   1. Silent renewal — an expired access token backed by a live refresh token
//      is rotated behind the scenes. The operator is never interrupted, and in
//      particular is never bounced to the login page on a browser reload.
//   2. Honest bounce — when renewal is impossible, the login page is reached
//      carrying the page the operator was on, and signing back in returns them
//      to it rather than to their default landing page.
//
// Storage keys (web/src/lib/auth/storage.ts):
//   "snooze-token" | "snooze-claims" | "snooze-refresh-token"

import { test, expect } from "../harness/fixtures";

const BOB_PW = "bob-pw";

/** Rewrites the stored JWT's `exp` into the past, leaving the signature alone.
 *  The server rejects it either way (bad signature / expired), which is exactly
 *  the state we want: an access token the API will refuse. */
function expireStoredToken(): void {
  const raw = window.localStorage.getItem("snooze-token");
  if (!raw) return;
  const [head, payload, sig] = raw.split(".");
  if (!payload) return;
  const b64 = payload.replace(/-/g, "+").replace(/_/g, "/");
  const claims = JSON.parse(atob(b64)) as Record<string, unknown>;
  claims["exp"] = Math.floor(Date.now() / 1000) - 60;
  const next = btoa(JSON.stringify(claims))
    .replace(/\+/g, "-")
    .replace(/\//g, "_")
    .replace(/=+$/, "");
  window.localStorage.setItem("snooze-token", `${head}.${next}.${sig}`);
  window.localStorage.setItem("snooze-claims", JSON.stringify(claims));
}

test.describe("session expiry", () => {
  test.beforeEach(async ({ api }) => {
    try {
      await api.users.create({
        name: "bob",
        method: "local",
        enabled: true,
        password: BOB_PW,
        roles: ["admin"],
        groups: [],
      });
    } catch (e: unknown) {
      const msg = e instanceof Error ? e.message : String(e);
      if (!msg.includes("409") && !msg.includes("already") && !msg.includes("duplicate")) throw e;
    }
  });

  test("an expired access token is renewed silently, without a trip to login", async ({
    page,
    api,
    server,
  }) => {
    const session = await api.loginSession("bob", BOB_PW);
    expect(session.refreshToken, "server must issue a refresh token").toBeTruthy();

    await page.addInitScript(
      ({ token, refreshToken }) => {
        // addInitScript runs on EVERY navigation, reloads included. Seeding
        // unconditionally would restore the pristine token on the reload below
        // and quietly un-expire the very thing under test.
        if (window.localStorage.getItem("snooze-token")) return;
        window.localStorage.setItem("snooze-token", token);
        window.localStorage.setItem("snooze-refresh-token", refreshToken!);
        const payload = token.split(".")[1]!;
        window.localStorage.setItem(
          "snooze-claims",
          atob(payload.replace(/-/g, "+").replace(/_/g, "/")),
        );
      },
      { token: session.token, refreshToken: session.refreshToken },
    );

    await page.goto(server.baseURL + "/web/alerts");
    await expect(page).toHaveURL(/\/web\/alerts/);

    // Age the access token out, then reload — the classic "came back to the tab
    // after lunch and hit refresh" case.
    await page.evaluate(expireStoredToken);
    const staleToken = await page.evaluate(() => window.localStorage.getItem("snooze-token"));
    let refreshCalls = 0;
    page.on("request", (r) => {
      if (r.url().includes("/api/v1/login/refresh")) refreshCalls++;
    });
    await page.reload();

    await expect(page).toHaveURL(/\/web\/alerts/);
    await expect(page).not.toHaveURL(/\/web\/login/);
    // A rotation actually happened rather than the page limping along stale.
    expect(refreshCalls, "the page must have called /login/refresh").toBeGreaterThan(0);
    await expect
      .poll(() => page.evaluate(() => window.localStorage.getItem("snooze-token")))
      .not.toBe(staleToken);
  });

  test("an unrenewable session lands on login and returns to the same page", async ({
    page,
    server,
  }) => {
    // No token at all: the deep link must survive the round trip through login.
    await page.goto(server.baseURL + "/web/rules");
    await expect(page).toHaveURL(/\/web\/login/);
    await expect(page).toHaveURL(/return_to/);

    await page.getByLabel("Username").fill("bob");
    await page.getByLabel("Password").fill(BOB_PW);
    await page.getByRole("button", { name: "Sign in" }).click();

    // Back where they were asked to sign in from — not the default landing page.
    await expect(page).toHaveURL(/\/web\/rules/);
  });

  test("logging out revokes the refresh token server-side", async ({ page, api, server }) => {
    const session = await api.loginSession("bob", BOB_PW);
    expect(session.refreshToken).toBeTruthy();

    await page.addInitScript(
      ({ token, refreshToken }) => {
        // addInitScript runs on EVERY navigation, reloads included. Seeding
        // unconditionally would restore the pristine token on the reload below
        // and quietly un-expire the very thing under test.
        if (window.localStorage.getItem("snooze-token")) return;
        window.localStorage.setItem("snooze-token", token);
        window.localStorage.setItem("snooze-refresh-token", refreshToken!);
        const payload = token.split(".")[1]!;
        window.localStorage.setItem(
          "snooze-claims",
          atob(payload.replace(/-/g, "+").replace(/_/g, "/")),
        );
      },
      { token: session.token, refreshToken: session.refreshToken },
    );

    await page.goto(server.baseURL + "/web/alerts");
    await expect(page).toHaveURL(/\/web\/alerts/);

    await page.getByRole("button", { name: /account menu — signed in as/i }).click({ force: true });
    // Wait for the revoke to actually land before probing, so the assertion
    // below is about revocation and not about a race with page teardown.
    const revoked = page.waitForResponse(
      (r) => r.url().includes("/api/v1/login/logout") && r.status() === 204,
    );
    await page.getByRole("menuitem", { name: "Log out" }).click({ force: true });
    await revoked;
    await expect(page).toHaveURL(/\/web\/login/);

    // The real test: clearing localStorage is not signing out. Until the server
    // is told, this token keeps minting access tokens for the rest of its lease
    // (7 days by default) for anyone who captured it.
    //
    // Exactly one probe — rotation revokes the presented token, so a retry loop
    // here would go green on its own second attempt whether or not logout did
    // anything.
    const probe = await api.ctx.post(`${server.baseURL}/api/v1/login/refresh`, {
      data: { refresh_token: session.refreshToken },
    });
    expect(probe.status()).toBe(401);
  });
});
