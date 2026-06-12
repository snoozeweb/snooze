// web/tests/e2e/harness/fixtures.ts
//
// Worker-scoped browser launched via CDP over a TCP port. Playwright's
// default --remote-debugging-pipe transport hangs under WSL2 (newPage
// never resolves), so we spawn full Chrome with --remote-debugging-port
// and connect via chromium.connectOverCDP(). Test-scoped `page` provides
// a fresh BrowserContext per test.
import {
  test as base,
  expect,
  chromium as pwChromium,
  type Browser,
  type Page,
} from "@playwright/test";
import { spawn, type ChildProcess } from "node:child_process";
import { startServer, type ServerHandle } from "./server";
import { mintRootToken, createApi, type SnoozeApi } from "./api";
import { createDbLauncher, type DbLauncher } from "./db";
import { loginAsAdmin } from "./auth";
import { freePort } from "./ports";

// Falls back to the Chromium binary that Playwright installed for this project.
const CHROME_BIN = process.env.E2E_CHROME_BIN ?? pwChromium.executablePath();

type WorkerFixtures = {
  dbLauncher: DbLauncher;
  server: ServerHandle;
  api: SnoozeApi;
  cdpBrowser: Browser;
};

type TestFixtures = {
  adminAuth: () => Promise<void>;
  page: Page;
};

export const test = base.extend<TestFixtures, WorkerFixtures>({
  dbLauncher: [
    async ({}, use) => {
      await use(createDbLauncher());
    },
    { scope: "worker" },
  ],

  server: [
    async ({ dbLauncher }, use, workerInfo) => {
      const worker = await dbLauncher.perWorker(workerInfo.workerIndex);
      const handle = await startServer({
        tmpdir: worker.tmpdir,
        env: worker.env,
        ...(worker.extraFlags !== undefined ? { extraFlags: worker.extraFlags } : {}),
      });
      try {
        await use(handle);
      } finally {
        await handle.stop();
        await dbLauncher.teardown(worker);
      }
    },
    { scope: "worker", timeout: 60_000 },
  ],

  api: [
    async ({ server }, use) => {
      const token = await mintRootToken({ adminSocketPath: server.adminSocketPath });
      const api = await createApi(server.baseURL, token);
      await use(api);
      await api.ctx.dispose();
    },
    { scope: "worker" },
  ],

  // One Chrome process per Playwright worker, launched via CDP port.
  cdpBrowser: [
    async ({}, use) => {
      const port = await freePort();
      const proc: ChildProcess = spawn(
        CHROME_BIN,
        [
          "--no-sandbox",
          "--headless=new",
          "--disable-dev-shm-usage",
          "--disable-gpu",
          "--disable-gpu-compositing",
          // Launching with a window-size large enough for the longest drawer
          // is what actually gives subsequent newPage() calls a usable viewport.
          // (connectOverCDP doesn't honour the project-level use.viewport, and
          // page.setViewportSize() against an attached browser is unreliable.)
          "--window-size=1600,1000",
          `--remote-debugging-port=${port}`,
          "--no-startup-window",
        ],
        { stdio: "ignore" },
      );
      let browser: Browser | null = null;
      for (let i = 0; i < 100; i++) {
        await new Promise<void>((r) => setTimeout(r, 100));
        try {
          const r = await fetch(`http://127.0.0.1:${port}/json/version`);
          if (r.ok) {
            browser = await pwChromium.connectOverCDP(`http://127.0.0.1:${port}`);
            break;
          }
        } catch {
          /* not ready yet */
        }
      }
      if (!browser) {
        proc.kill("SIGKILL");
        throw new Error(`Chrome did not become ready (port ${port})`);
      }
      try {
        await use(browser);
      } finally {
        await browser.close().catch(() => {});
        proc.kill("SIGTERM");
      }
    },
    { scope: "worker", timeout: 30_000 },
  ],

  // Test-scoped: fresh BrowserContext + Page per test. Viewport comes from
  // the Chrome window size set in --window-size above (connectOverCDP
  // bypasses the project's `use.viewport` setting). We also inject a global
  // stylesheet to disable CSS animations & transitions — Radix Drawer's
  // slide-in animation otherwise leaves elements mid-flight when Playwright
  // measures their bounding box, producing flaky "outside the viewport"
  // failures for footer buttons.
  page: async ({ cdpBrowser }, use) => {
    const ctx = await cdpBrowser.newContext();
    const pg = await ctx.newPage();
    await pg.addInitScript(() => {
      const style = document.createElement("style");
      style.textContent =
        "*, *::before, *::after { animation: none !important; transition: none !important; }";
      // Inject at first paint so even Radix's opening keyframes are no-ops.
      const insert = () => document.head?.appendChild(style);
      if (document.head) insert();
      else document.addEventListener("DOMContentLoaded", insert);
    });
    try {
      await use(pg);
    } finally {
      await ctx.close().catch(() => {});
    }
  },

  adminAuth: async ({ page, api, server }, use) => {
    await use(async () => {
      await loginAsAdmin(page, { baseURL: server.baseURL, token: api.token });
    });
  },
});

export { expect };
