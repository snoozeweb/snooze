// web/tests/e2e/auth/cross-tab.spec.ts
//
// Cross-tab logout test.
//
// How the cross-tab listener works (web/src/lib/auth/store.ts, lines 54–60):
//
//   window.addEventListener("storage", (e) => {
//     if (e.key === "snooze-token") {
//       authStore.getState().refresh();
//     }
//   });
//
// Strategy:
//   1. Seed both pages with a valid token via addInitScript.
//   2. Navigate both to /web/ — both should land on /web/alerts.
//   3. In tab A: remove both storage keys.
//   4. Dispatch a StorageEvent for "snooze-token" in tab B's context — this
//      mirrors what the browser sends to all *other* tabs when a key changes.
//      The store's listener calls refresh() → sees no token → unauthenticated
//      → router redirects to /web/login.
//
// Storage keys (web/src/lib/auth/storage.ts):
//   "snooze-token"  — raw JWT (the key the store's listener watches)
//   "snooze-claims" — decoded payload (cleared together for a clean logout)
//
// User creation:
//   The /api/v1/user endpoint runs the user plugin's WriteTransformer, which
//   bcrypt-hashes a non-empty plaintext `password` on write (same path the web
//   UI uses). So we seed the PLAINTEXT password + method:"local" + enabled:true;
//   the stored hash is what the local-auth provider verifies at login.
//   No api.reset() — each worker has an isolated fresh DB.
//
// WSL2 / headless_shell workaround:
//   Same as login.spec.ts: launch regular Chrome with --remote-debugging-port
//   and connect via connectOverCDP to work around WSL2 pipe IPC issues.

import { test, expect } from "../harness/fixtures";

// ────────────────────────────────────────────────────────────────────────────
// alice's plaintext password — same as login.spec.ts. The user plugin's
// WriteTransformer bcrypt-hashes it server-side on create.
// ────────────────────────────────────────────────────────────────────────────
const ALICE_PW = "alice-pw";

// Mirrors the addInitScript body in harness/auth.ts: writes snooze-token +
// snooze-claims so the store considers the session authenticated.
const seedStorage = (t: string) => {
  window.localStorage.setItem("snooze-token", t);
  try {
    const payload = t.split(".")[1];
    if (payload) {
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
    // If decode fails the store will fall back to decoding the token itself.
  }
};

test("logout in one tab logs out the other", async ({ cdpBrowser, api, server }) => {
  // Each worker has an isolated fresh DB — no cleanup needed.
  await api.users.create({
    name: "alice",
    method: "local",
    enabled: true,
    password: ALICE_PW,
    roles: ["admin"],
    groups: [],
  });
  const token = await api.loginLocal("alice", ALICE_PW);

  // Use a shared BrowserContext so both pages share localStorage (same origin).
  const ctx = await cdpBrowser.newContext();
  const pageA = await ctx.newPage();
  const pageB = await ctx.newPage();

  // Seed both pages with the same valid token before any navigation.
  await pageA.addInitScript(seedStorage, token);
  await pageB.addInitScript(seedStorage, token);

  await pageA.goto(server.baseURL + "/web/");
  await pageB.goto(server.baseURL + "/web/");

  await expect(pageA).toHaveURL(/\/web\/alerts/);
  await expect(pageB).toHaveURL(/\/web\/alerts/);

  // ── Simulate logout from tab A ────────────────────────────────────────────

  // Step 1: Clear auth storage in A (and therefore in the shared localStorage).
  await pageA.evaluate(() => {
    window.localStorage.removeItem("snooze-token");
    window.localStorage.removeItem("snooze-claims");
  });

  // Step 2: Dispatch the storage event in B as the browser would when another
  // tab removes a key. The store listener checks e.key === "snooze-token" and
  // calls refresh(), which reads no token and marks the session as logged out.
  await pageB.evaluate(() => {
    window.dispatchEvent(
      new StorageEvent("storage", {
        key: "snooze-token",
        newValue: null,
        storageArea: window.localStorage,
      }),
    );
  });

  // B should redirect to the login page within a short timeout.
  await expect.poll(() => pageB.url(), { timeout: 3_000 }).toMatch(/\/web\/login/);

  await ctx.close();
});
