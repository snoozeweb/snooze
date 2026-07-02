import { describe, expect, it } from "vitest";
import { envColor } from "./environment-color";

describe("envColor", () => {
  it("is stable for a given name", () => {
    expect(envColor("production")).toBe(envColor("production"));
  });

  it("returns a hex colour from the palette, never black", () => {
    for (const name of ["production", "staging", "dev", "qa", "canary"]) {
      expect(envColor(name)).toMatch(/^#[0-9a-f]{6}$/i);
      expect(envColor(name).toLowerCase()).not.toBe("#000000");
    }
  });

  it("distinguishes at least a couple of common names", () => {
    // Not guaranteed for every pair, but the palette should separate these.
    expect(envColor("production")).not.toBe(envColor("staging"));
  });
});
