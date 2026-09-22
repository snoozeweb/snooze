import { renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { authStore } from "@/lib/auth/store";
import { ANALYSIS_WRITE_PERM, useCanWriteAnalysis } from "./perms";

function loginWithPerms(perms: string[]) {
  const header = btoa(JSON.stringify({ alg: "HS256", typ: "JWT" }));
  const body = btoa(
    JSON.stringify({
      sub: "tester",
      exp: Math.floor(Date.now() / 1000) + 3600,
      permissions: perms,
    }),
  );
  authStore.getState().login(`${header}.${body}.sig`);
}

function canWrite(): boolean {
  // Unmount before returning: the hook subscribes to the auth store, and the
  // afterEach logout below would otherwise push a state update into a still
  // -mounted component outside act().
  const { result, unmount } = renderHook(() => useCanWriteAnalysis());
  const value = result.current;
  unmount();
  return value;
}

describe("useCanWriteAnalysis", () => {
  afterEach(() => {
    authStore.getState().logout({ revoke: false });
  });

  it("names the permission the way the server does", () => {
    expect(ANALYSIS_WRITE_PERM).toBe("rw_protected");
  });

  it("grants the holder of the literal permission", () => {
    loginWithPerms(["ro_record", ANALYSIS_WRITE_PERM]);
    expect(canWrite()).toBe(true);
  });

  it("refuses the rw_all wildcard, exactly like the server's route does", () => {
    // The whole reason this hook exists instead of hasAnyPermission: an admin
    // holding rw_all sees the analysis read-only. Showing them an enabled Save
    // button would hand them a 403 after they had filled the form in.
    loginWithPerms(["rw_all"]);
    expect(canWrite()).toBe(false);
  });

  it("refuses a near miss and an unrelated permission", () => {
    loginWithPerms(["ro_protected", "rw_record"]);
    expect(canWrite()).toBe(false);
  });

  it("refuses when there are no claims at all", () => {
    authStore.getState().logout({ revoke: false });
    expect(authStore.getState().claims).toBeNull();
    expect(canWrite()).toBe(false);
  });
});
