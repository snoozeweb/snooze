// web/tests/e2e/harness/db/mongo.ts
import type { DbLauncher } from "./types";

export function mongoLauncher(): DbLauncher {
  return {
    driver: "mongo",
    // eslint-disable-next-line @typescript-eslint/require-await
    async perWorker() {
      throw new Error(
        "mongo E2E launcher not yet implemented — set SNOOZE_TEST_DB=sqlite or provide an impl",
      );
    },
    async teardown() {},
  };
}
