// web/tests/e2e/harness/db/postgres.ts
//
// Session-scoped PostgreSQL container, shared across all Playwright workers.
//
// Lifecycle:
//   globalSetup      → startPostgresSession()  → docker run postgres:16-alpine
//                                              → poll pg_isready
//                                              → write .bin/postgres-conn.json
//   perWorker(i)     → read .bin/postgres-conn.json
//                    → DROP + CREATE DATABASE snooze_e2e_w<i>
//                    → write cfg/core.yaml pointing at the per-worker DSN
//   teardown(worker) → rm tmpdir  (container outlives the worker)
//   globalTeardown   → stopPostgresSession() → docker kill the container
//
// Worker isolation is per-database. The single postgres instance is shared.
import { mkdtemp, mkdir, writeFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import type { DbLauncher, WorkerDbConfig } from "./types";
import {
  poll,
  readConn,
  removeConn,
  resolveHostPort,
  run,
  runAllowFail,
  SESSION_LABEL,
  sweepOrphans,
  writeConn,
  type ConnInfo,
} from "./shared";

const POSTGRES_IMAGE = "postgres:16-alpine";
const CONTAINER_PORT = 5432;

async function pgIsReady(containerId: string): Promise<boolean> {
  try {
    await run(
      "docker",
      [
        "exec",
        containerId,
        "pg_isready",
        "-U",
        "snooze",
        "-d",
        "snooze",
        "-h",
        "127.0.0.1",
      ],
      5_000,
    );
    return true;
  } catch {
    return false;
  }
}

/**
 * Start a single postgres:16-alpine container for the test session.
 * Idempotent: re-running before stopPostgresSession will tear down and recreate.
 */
export async function startPostgresSession(): Promise<ConnInfo> {
  // Belt-and-suspenders: kill any orphans from previous runs.
  await sweepOrphans();

  const containerName = `snooze-e2e-postgres-${process.pid}-${Date.now()}`;
  const containerId = await run(
    "docker",
    [
      "run",
      "-d",
      "--rm",
      "--label",
      SESSION_LABEL,
      "--label",
      `snooze-e2e-session=${process.pid}`,
      "-p",
      `0:${CONTAINER_PORT}`,
      "-e",
      "POSTGRES_USER=snooze",
      "-e",
      "POSTGRES_PASSWORD=snooze",
      "-e",
      "POSTGRES_DB=snooze",
      "--name",
      containerName,
      POSTGRES_IMAGE,
    ],
    60_000,
  );

  try {
    const hostPort = await poll(
      async () => {
        try {
          return await resolveHostPort(containerId, CONTAINER_PORT);
        } catch {
          return null;
        }
      },
      15_000,
      500,
      "docker port mapping",
    );

    await poll(() => pgIsReady(containerId), 60_000, 1_000, "pg_isready");

    const uri = `postgres://snooze:snooze@127.0.0.1:${hostPort}/snooze?sslmode=disable`;
    const info: ConnInfo = { containerId, containerName, hostPort, uri };
    await writeConn("postgres", info);
    return info;
  } catch (err) {
    await runAllowFail("docker", ["kill", containerId], 15_000);
    throw err;
  }
}

export async function stopPostgresSession(): Promise<void> {
  try {
    const info = await readConn("postgres");
    await runAllowFail("docker", ["kill", info.containerId], 15_000);
  } catch {
    // No conn file → nothing to stop.
  }
  // Sweep any leftovers (e.g. orphans from previous crashed runs).
  await sweepOrphans();
  await removeConn("postgres");
}

export function postgresLauncher(): DbLauncher {
  return {
    driver: "postgres",

    async perWorker(workerIndex) {
      const info = await readConn("postgres");
      const dir = await mkdtemp(
        join(tmpdir(), `snooze-e2e-postgres-w${workerIndex}-`),
      );
      await mkdir(join(dir, "cfg"), { recursive: true });

      const dbName = `snooze_e2e_w${workerIndex}`;

      // Drop-and-recreate for clean state if a worker restarts in the same session.
      await run(
        "docker",
        [
          "exec",
          info.containerId,
          "psql",
          "-U",
          "snooze",
          "-d",
          "snooze",
          "-v",
          "ON_ERROR_STOP=1",
          "-c",
          `DROP DATABASE IF EXISTS ${dbName};`,
        ],
        15_000,
      );
      await run(
        "docker",
        [
          "exec",
          info.containerId,
          "psql",
          "-U",
          "snooze",
          "-d",
          "snooze",
          "-v",
          "ON_ERROR_STOP=1",
          "-c",
          `CREATE DATABASE ${dbName};`,
        ],
        15_000,
      );

      // Build per-worker DSN: replace the db name in the base URI.
      const workerDsn = info.uri.replace(/\/snooze\?/, `/${dbName}?`);

      const coreYaml =
        [
          "database:",
          "  type: postgres",
          `  dsn: "${workerDsn}"`,
        ].join("\n") + "\n";

      await writeFile(join(dir, "cfg", "core.yaml"), coreYaml, "utf-8");
      return { tmpdir: dir, env: {} };
    },

    async teardown(worker: WorkerDbConfig) {
      // Container is session-scoped — only clean up the worker tmpdir here.
      await rm(worker.tmpdir, { recursive: true, force: true });
    },
  };
}
