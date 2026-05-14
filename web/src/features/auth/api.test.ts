import { describe, expect, it } from "vitest";
import { loginAnonymous, loginLdap, loginLocal } from "./api";

describe("login API", () => {
  it("loginLocal returns a token from MSW", async () => {
    const token = await loginLocal({ username: "alice", password: "secret" });
    expect(typeof token).toBe("string");
    expect(token.split(".").length).toBe(3);
  });

  it("loginLdap returns a token from MSW", async () => {
    const token = await loginLdap({ username: "alice", password: "secret" });
    expect(token.split(".").length).toBe(3);
  });

  it("loginAnonymous returns a token from MSW", async () => {
    const token = await loginAnonymous();
    expect(token.split(".").length).toBe(3);
  });
});
