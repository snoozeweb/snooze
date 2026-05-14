// web/tests/e2e/harness/ports.ts
import { createServer } from "node:net";

export async function freePort(): Promise<number> {
  return new Promise((resolveP, reject) => {
    const srv = createServer();
    srv.unref();
    srv.on("error", reject);
    srv.listen(0, "127.0.0.1", () => {
      const addr = srv.address();
      if (typeof addr !== "object" || addr === null) {
        reject(new Error("could not allocate port"));
        return;
      }
      const port = addr.port;
      srv.close(() => resolveP(port));
    });
  });
}
