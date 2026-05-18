// web/tests/e2e/global-teardown.ts
import { stopMongoSession } from "./harness/db/mongo";
import { stopPostgresSession } from "./harness/db/postgres";

export default async function globalTeardown(): Promise<void> {
  const driver = process.env.SNOOZE_TEST_DB ?? "sqlite";
  if (driver === "mongo") {
    await stopMongoSession();
  } else if (driver === "postgres") {
    await stopPostgresSession();
  }
}
