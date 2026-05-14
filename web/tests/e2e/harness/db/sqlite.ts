// web/tests/e2e/harness/db/sqlite.ts
//
// NOTE: snooze-server's file-based backend is called "file" in the config
// schema (oneof=mongo file postgres). The "sqlite" driver name is used only
// in this harness as an abstraction label; the actual config value is "file".
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
      const dbPath = join(dir, "db.json");
      // core.yaml keys are loaded under the "core" section (sectionFiles loader).
      // The Database.Type field uses validate:"oneof=mongo file postgres", so the
      // correct value is "file", not "sqlite".
      await writeFile(
        join(dir, "cfg", "core.yaml"),
        `database:\n  type: file\n  path: ${dbPath}\n`,
        "utf-8",
      );
      return {
        tmpdir: dir,
        env: {
          // Belt-and-suspenders override via env vars (envKeyToPath mapping).
          SNOOZE_SERVER_CORE_DATABASE_TYPE: "file",
          SNOOZE_SERVER_CORE_DATABASE_PATH: dbPath,
        },
      };
    },
    async teardown(worker: WorkerDbConfig) {
      await rm(worker.tmpdir, { recursive: true, force: true });
    },
  };
}
