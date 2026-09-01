import { describe, expect, it } from "vitest";
import { ApiError } from "./client";
import { describeActionError, describeError } from "./errorMessage";

describe("describeError", () => {
  it("translates a 401 into the app's own voice, not the backend's", () => {
    const err = new ApiError(401, "unauthorized", "invalid credentials");
    expect(describeError(err).summary).toBe("Wrong username or password.");
  });

  it("keeps the raw detail as secondary on a 5xx, with a human summary", () => {
    const err = new ApiError(500, "internal", "internal server error");
    const { summary, secondary } = describeError(err);
    expect(summary).toBe("The server ran into a problem.");
    expect(secondary).toBe("internal server error");
  });

  it("sentence-cases an ordinary 4xx detail", () => {
    const err = new ApiError(409, "conflict", "name already exists");
    expect(describeError(err).summary).toBe("Name already exists.");
  });

  it("gives a connectivity message for a network failure (no Response)", () => {
    const err = new TypeError("Failed to fetch");
    expect(describeError(err).summary).toMatch(/couldn't reach the server/i);
  });

  it("falls back to the caller-supplied default for an unrecognized error", () => {
    expect(describeError("boom", "Save failed").summary).toBe("Save failed");
  });
});

describe("describeActionError", () => {
  it("names the verb and subject, folding the cause into one sentence", () => {
    const err = new ApiError(500, "internal", "internal server error");
    const { summary, secondary } = describeActionError("acknowledge", "srv-prod-db-01", err);
    expect(summary).toBe(
      "Couldn't acknowledge srv-prod-db-01 — the server ran into a problem.",
    );
    expect(secondary).toBe("internal server error");
  });
});
