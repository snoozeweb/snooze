// web/tests/e2e/harness/db/postgres.ts
import type { DbLauncher } from "./types";

export function postgresLauncher(): DbLauncher {
  return {
    driver: "postgres",
    async perWorker() {
      throw new Error(
        "postgres E2E launcher not yet implemented — set SNOOZE_TEST_DB=sqlite or provide an impl",
      );
    },
    async teardown() {},
  };
}
