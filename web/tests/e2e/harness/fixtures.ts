// web/tests/e2e/harness/fixtures.ts
import { test as base, expect } from "@playwright/test";
import { startServer, type ServerHandle } from "./server";
import { mintRootToken, createApi, type SnoozeApi } from "./api";
import { createDbLauncher, type DbLauncher } from "./db";
import { loginAsAdmin } from "./auth";

type WorkerFixtures = {
  dbLauncher: DbLauncher;
  server: ServerHandle;
  api: SnoozeApi;
};

type TestFixtures = {
  adminAuth: () => Promise<void>;
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

  adminAuth: async ({ page, api, server }, use) => {
    await use(async () => {
      await loginAsAdmin(page, { baseURL: server.baseURL, token: api.token });
    });
  },
});

export { expect };
