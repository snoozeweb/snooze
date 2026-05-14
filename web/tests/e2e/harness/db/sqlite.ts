// web/tests/e2e/harness/db/sqlite.ts
import { mkdtemp, rm, mkdir, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import type { DbLauncher, WorkerDbConfig } from "./types";

export function sqliteLauncher(): DbLauncher {
  return {
    driver: "sqlite",
    async perWorker(workerIndex) {
      const dir = await mkdtemp(join(tmpdir(), `snooze-e2e-w${workerIndex}-`));
      await mkdir(join(dir, "cfg"), { recursive: true });
      // Minimal core.yaml that picks sqlite + bind path.
      await writeFile(
        join(dir, "cfg", "core.yaml"),
        `database:\n  type: sqlite\n  path: ${join(dir, "db.sqlite")}\n`,
        "utf-8",
      );
      return {
        tmpdir: dir,
        env: {
          // Belt-and-suspenders override via env (sectionFiles loader respects env too).
          SNOOZE_SERVER_CORE_DATABASE_TYPE: "sqlite",
          SNOOZE_SERVER_CORE_DATABASE_PATH: join(dir, "db.sqlite"),
        },
      };
    },
    async teardown(worker: WorkerDbConfig) {
      await rm(worker.tmpdir, { recursive: true, force: true });
    },
  };
}
