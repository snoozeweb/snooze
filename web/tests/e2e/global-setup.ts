// web/tests/e2e/global-setup.ts
import { execSync } from "node:child_process";
import { existsSync, mkdirSync } from "node:fs";
import { resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { startMongoSession } from "./harness/db/mongo";
import { startPostgresSession } from "./harness/db/postgres";

const __dirname = dirname(fileURLToPath(import.meta.url));

export default async function globalSetup(): Promise<void> {
  const repoRoot = resolve(__dirname, "../../..");
  const webDir = resolve(repoRoot, "web");
  const binDir = resolve(webDir, "tests/e2e/.bin");
  const distDir = resolve(webDir, "dist");

  if (process.env.E2E_SKIP_BUILD !== "1") {
    mkdirSync(binDir, { recursive: true });

    if (!existsSync(resolve(distDir, "index.html"))) {
      execSync("npm run build", { cwd: webDir, stdio: "inherit" });
    }
    execSync(`go build -o ${binDir}/snooze-server ./cmd/snooze-server`, {
      cwd: repoRoot,
      stdio: "inherit",
    });
  }

  // Provision the backend container once for the whole session.
  // SNOOZE_TEST_DB defaults to "sqlite" in createDbLauncher() — only mongo
  // and postgres need a container.
  const driver = process.env.SNOOZE_TEST_DB ?? "sqlite";
  if (driver === "mongo") {
    await startMongoSession();
  } else if (driver === "postgres") {
    await startPostgresSession();
  }
}
