// web/tests/e2e/harness/db/mongo.ts
//
// Session-scoped MongoDB container, shared across all Playwright workers.
//
// Lifecycle:
//   globalSetup     → startMongoSession()  → docker run mongo:7 --replSet rs0
//                                          → rs.initiate(), wait PRIMARY
//                                          → write .bin/mongo-conn.json
//   perWorker(i)    → read .bin/mongo-conn.json
//                   → use database `snooze_e2e_w<i>` on the shared container
//                   → write cfg/core.yaml pointing at the URI
//   teardown(worker) → rm tmpdir  (container outlives the worker)
//   globalTeardown  → stopMongoSession() → docker kill the container
//
// Worker isolation is per-database, not per-container. The replica set is
// shared. Change streams are scoped per database, so worker A doesn't see
// worker B's events.
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

const MONGO_IMAGE = "mongo:7";
const CONTAINER_PORT = 27017;

async function pingMongo(containerId: string): Promise<boolean> {
  try {
    const out = await run(
      "docker",
      [
        "exec",
        containerId,
        "mongosh",
        "--quiet",
        "--eval",
        "JSON.stringify(db.runCommand({ping:1}))",
      ],
      5_000,
    );
    const parsed = JSON.parse(out) as { ok?: number };
    return parsed.ok === 1;
  } catch {
    return false;
  }
}

async function initReplicaSet(containerId: string): Promise<void> {
  await runAllowFail(
    "docker",
    [
      "exec",
      containerId,
      "mongosh",
      "--quiet",
      "--eval",
      `try {
         var s = rs.status();
         if (s.myState !== 1) { throw new Error("not primary yet"); }
       } catch(e) {
         if (e.codeName === 'NotYetInitialized' || /no replset config/i.test(e.message)) {
           rs.initiate({_id:'rs0',members:[{_id:0,host:'127.0.0.1:27017'}]});
         }
       }`,
    ],
    10_000,
  );

  await poll(
    async () => {
      try {
        const out = await run(
          "docker",
          [
            "exec",
            containerId,
            "mongosh",
            "--quiet",
            "--eval",
            "JSON.stringify({myState: rs.status().myState})",
          ],
          5_000,
        );
        const parsed = JSON.parse(out) as { myState?: number };
        return parsed.myState === 1 ? true : null;
      } catch {
        return null;
      }
    },
    45_000,
    2_000,
    "replica-set PRIMARY",
  );
}

/**
 * Start a single mongo:7 container and prepare it for the test session.
 * Idempotent: re-running before stopMongoSession will tear down and recreate.
 */
export async function startMongoSession(): Promise<ConnInfo> {
  // Belt-and-suspenders: kill any orphans from previous runs.
  await sweepOrphans();

  const containerName = `snooze-e2e-mongo-${process.pid}-${Date.now()}`;
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
      "--name",
      containerName,
      MONGO_IMAGE,
      "--replSet",
      "rs0",
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

    await poll(() => pingMongo(containerId), 60_000, 1_000, "mongod ping");
    await initReplicaSet(containerId);

    const uri = `mongodb://127.0.0.1:${hostPort}/?directConnection=true&replicaSet=rs0`;
    const info: ConnInfo = { containerId, containerName, hostPort, uri };
    await writeConn("mongo", info);
    return info;
  } catch (err) {
    await runAllowFail("docker", ["kill", containerId], 15_000);
    throw err;
  }
}

export async function stopMongoSession(): Promise<void> {
  try {
    const info = await readConn("mongo");
    await runAllowFail("docker", ["kill", info.containerId], 15_000);
  } catch {
    // No conn file → nothing to stop.
  }
  // Sweep any leftovers (e.g. orphans from previous crashed runs).
  await sweepOrphans();
  await removeConn("mongo");
}

export function mongoLauncher(): DbLauncher {
  return {
    driver: "mongo",

    async perWorker(workerIndex) {
      const info = await readConn("mongo");
      const dir = await mkdtemp(join(tmpdir(), `snooze-e2e-mongo-w${workerIndex}-`));
      await mkdir(join(dir, "cfg"), { recursive: true });

      const dbName = `snooze_e2e_w${workerIndex}`;
      const coreYaml = [
        "database:",
        "  type: mongo",
        `  host: "${info.uri}"`,
        `  database: ${dbName}`,
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
