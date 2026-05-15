import { describe, expect, it } from "vitest";
import { loginAnonymous, loginLdap, loginLocal, postLogout, postRefresh } from "./api";

describe("login API", () => {
  it("loginLocal returns a token + refresh token from MSW", async () => {
    const result = await loginLocal({ username: "alice", password: "secret" });
    expect(typeof result.token).toBe("string");
    expect(result.token.split(".").length).toBe(3);
    expect(result.refreshToken).toMatch(/^refresh-alice-/);
  });

  it("loginLdap returns a token + refresh token from MSW", async () => {
    const result = await loginLdap({ username: "alice", password: "secret" });
    expect(result.token.split(".").length).toBe(3);
    expect(result.refreshToken).toMatch(/^refresh-alice-/);
  });

  it("loginAnonymous returns a token + refresh token from MSW", async () => {
    const result = await loginAnonymous();
    expect(result.token.split(".").length).toBe(3);
    expect(result.refreshToken).toMatch(/^refresh-anonymous-/);
  });

  it("postRefresh exchanges a refresh token for a new pair", async () => {
    const refreshed = await postRefresh("seed-refresh");
    expect(refreshed.token.split(".").length).toBe(3);
    expect(refreshed.refreshToken).toMatch(/^refresh-alice-/);
  });

  it("postLogout never throws", async () => {
    await expect(postLogout("seed-refresh")).resolves.toBeUndefined();
    await expect(postLogout(null)).resolves.toBeUndefined();
  });
});
