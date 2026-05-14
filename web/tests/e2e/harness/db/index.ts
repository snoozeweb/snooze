// web/tests/e2e/harness/db/index.ts
import { sqliteLauncher } from "./sqlite";
import { postgresLauncher } from "./postgres";
import { mongoLauncher } from "./mongo";
import type { DbLauncher } from "./types";

export function createDbLauncher(): DbLauncher {
  const driver = (process.env.SNOOZE_TEST_DB ?? "sqlite") as "sqlite" | "postgres" | "mongo";
  switch (driver) {
    case "sqlite": return sqliteLauncher();
    case "postgres": return postgresLauncher();
    case "mongo": return mongoLauncher();
    default:
      throw new Error(`unknown SNOOZE_TEST_DB: ${driver as string}`);
  }
}

export type { DbLauncher, WorkerDbConfig } from "./types";
