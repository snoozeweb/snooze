// web/tests/e2e/auth/login.spec.ts
//
// Selectors derived from actual implementation:
//   - Login form: web/src/features/auth/Login.tsx
//       Username input: id="login-username-local", label "Username"
//       Password input: id="login-password-local", label "Password"
//       Submit: <Button type="submit">Sign in</Button>
//       Error container: role="alert"
//   - User menu: web/src/app/layout/Topbar.tsx
//       Trigger: <IconButton label={`Signed in as ${username}`} />
//       Logout: <MenuItem>Log out</MenuItem>
//
// Storage keys (web/src/lib/auth/storage.ts):
//   "snooze-token"  — raw JWT string
//   "snooze-claims" — JSON-stringified decoded JWT payload
//
// User creation:
//   The /api/v1/user endpoint stores documents as-is. The local-auth provider
//   (internal/auth/local.go) expects a bcrypt-hashed password. We therefore
//   pass a pre-computed bcrypt hash for "alice-pw" (cost 10) rather than the
//   plaintext. The document must also include method:"local" and enabled:true,
//   mirroring what bootstrap.go writes for the root user.
//
// WSL2 / headless_shell workaround:
//   Playwright 1.49 uses --remote-debugging-pipe by default for Chromium.
//   In WSL2, the renderer process cannot communicate back through that pipe,
//   causing ctx.newPage() to hang indefinitely. The fix is to launch the
//   regular chrome binary (not headless_shell) with --remote-debugging-port
//   and connect via chromium.connectOverCDP(). We override the `browser`
//   and `page` fixtures here to do exactly that.
//
// NOTE: api.reset() is intentionally NOT called. Each Playwright worker gets
// an isolated fresh DB (file-backed), so no cross-test pollution exists.
// The alice user is created once per worker (beforeEach is idempotent).

import { chromium as pwChromium, expect, type Browser, type Page } from "@playwright/test";
import { test as baseTest } from "../harness/fixtures";
import { loginAsAdmin } from "../harness/auth";
import { spawn } from "node:child_process";
import { freePort } from "../harness/ports";

// Regular Chrome binary — works with port-based CDP in WSL2.
const CHROME_BIN = "/home/nemega/.cache/ms-playwright/chromium-1148/chrome-linux/chrome";

// Fixtures: worker-scoped browser (one Chrome per worker) +
//           test-scoped page (fresh context per test).
type ExtraWorker = { cdpBrowser: Browser };
type ExtraTest = { page: Page };

const test = baseTest.extend<ExtraTest, ExtraWorker>({
  // Worker-scoped: one Chrome process per Playwright worker.
  cdpBrowser: [
    async ({}, use) => {
      const port = await freePort();
      const proc = spawn(CHROME_BIN, [
        "--no-sandbox",
        "--headless=new",
        "--disable-dev-shm-usage",
        "--disable-gpu",
        "--disable-gpu-compositing",
        `--remote-debugging-port=${port}`,
        "--no-startup-window",
      ], { stdio: "ignore" });
      // Poll until Chrome DevTools is ready.
      let browser: Browser | null = null;
      for (let i = 0; i < 60; i++) {
        await new Promise<void>((r) => setTimeout(r, 100));
        try {
          const r = await fetch(`http://127.0.0.1:${port}/json/version`);
          if (r.ok) {
            browser = await pwChromium.connectOverCDP(`http://127.0.0.1:${port}`);
            break;
          }
        } catch { /* not ready yet */ }
      }
      if (!browser) throw new Error("Chrome did not become ready in time");
      try {
        await use(browser);
      } finally {
        await browser.close().catch(() => {});
        proc.kill("SIGTERM");
      }
    },
    { scope: "worker" },
  ],

  // Test-scoped: fresh BrowserContext + Page per test.
  page: async ({ cdpBrowser }, use) => {
    const ctx = await cdpBrowser.newContext();
    const pg = await ctx.newPage();
    try {
      await use(pg);
    } finally {
      await ctx.close().catch(() => {});
    }
  },
});

// ────────────────────────────────────────────────────────────────────────────
// bcrypt hash for "alice-pw" (cost 10).
// Generated via: go run -modfile=go.mod -run . <inline program using bcrypt>
// Verified: bcrypt.CompareHashAndPassword(hash, "alice-pw") == nil
// ────────────────────────────────────────────────────────────────────────────
const ALICE_PW = "alice-pw";
const ALICE_PW_HASH = "$2a$10$xcejb8fDY6p6MAkj7JtDXuyL/fE.NoSRY3DkuF03OdmF7CMBnowCu";

test.describe("login (local)", () => {
  // Create the alice user before each test. Idempotent: 409-conflict errors
  // (alice already exists from a prior test in this worker) are swallowed.
  test.beforeEach(async ({ api }) => {
    try {
      await api.users.create({
        name: "alice",
        method: "local",
        enabled: true,
        password: ALICE_PW_HASH,
        roles: ["admin"],
        groups: [],
      });
    } catch (e: unknown) {
      const msg = e instanceof Error ? e.message : String(e);
      if (!msg.includes("409") && !msg.includes("already") && !msg.includes("duplicate")) {
        throw e;
      }
    }
  });

  // ── Happy path ─────────────────────────────────────────────────────────────

  test("happy path lands on Alerts", async ({ page, server }) => {
    await page.goto(server.baseURL + "/web/");

    // The local tab is active by default. Labels come from Login.tsx.
    await page.getByLabel("Username").fill("alice");
    await page.getByLabel("Password").fill(ALICE_PW);
    await page.getByRole("button", { name: "Sign in" }).click({ force: true });

    await expect(page).toHaveURL(/\/web\/alerts/);
    // Topbar breadcrumb / page heading for the alerts route.
    await expect(page.getByText(/alerts/i).first()).toBeVisible();
  });

  // ── Wrong password ─────────────────────────────────────────────────────────

  test("wrong password surfaces error", async ({ page, server }) => {
    await page.goto(server.baseURL + "/web/");

    await page.getByLabel("Username").fill("alice");
    await page.getByLabel("Password").fill("definitely-wrong");
    await page.getByRole("button", { name: "Sign in" }).click({ force: true });

    // Login.tsx renders <div role="alert"> when the API returns an error.
    await expect(page.getByRole("alert")).toBeVisible();
    // Must still be on the login page, not the alerts page.
    await expect(page).not.toHaveURL(/\/web\/alerts/);
  });

  // ── Empty form ─────────────────────────────────────────────────────────────

  test("empty form shows browser validation or keeps button enabled with no nav", async ({
    page,
    server,
  }) => {
    await page.goto(server.baseURL + "/web/");

    // The inputs have `required` (HTML5 constraint validation).
    // A click without filling should either keep the page on login (HTML5
    // prevents submission) or the button is disabled. Either is acceptable.
    const btn = page.getByRole("button", { name: "Sign in" });
    const isDisabled = await btn.isDisabled();
    if (!isDisabled) {
      await btn.click({ force: true });
      // Constraint validation prevents submission — URL stays on login.
      await expect(page).not.toHaveURL(/\/web\/alerts/);
    }
  });

  // ── Already-authenticated redirect ────────────────────────────────────────

  test("already-authenticated user is redirected past login to Alerts", async ({
    page,
    api,
    server,
  }) => {
    // Get a real JWT via loginLocal, then seed storage the same way
    // web/src/lib/auth/storage.ts does it (via loginAsAdmin helper).
    const userToken = await api.loginLocal("alice", ALICE_PW);

    // loginAsAdmin writes "snooze-token" + "snooze-claims" — matching storage.ts.
    await loginAsAdmin(page, { baseURL: server.baseURL, token: userToken });

    // The router sees valid auth and redirects to /web/alerts.
    await expect(page).toHaveURL(/\/web\/alerts/);
  });

  // ── Logout flow ────────────────────────────────────────────────────────────

  test("logout returns to login", async ({ page, adminAuth }) => {
    // adminAuth writes the root token and navigates to /web/.
    await adminAuth();
    await expect(page).toHaveURL(/\/web\/alerts/);

    // Topbar.tsx: <IconButton icon="users" label={`Signed in as ${username}`} />
    // The root token's sub claim is "root".
    // Radix DropdownMenu renders two button elements — use getByLabel to
    // target the visible icon button specifically (avoids strict-mode error).
    await page.getByLabel(/signed in as/i).click({ force: true });

    // MenuItem text is "Log out" (exactly, from Topbar.tsx).
    await page.getByRole("menuitem", { name: "Log out" }).click({ force: true });

    await expect(page).toHaveURL(/\/web\/login/);
  });
});

export { expect };
