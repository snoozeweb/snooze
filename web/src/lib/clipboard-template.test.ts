import { describe, expect, it } from "vitest";
import { expandTemplate } from "./clipboard-template";

describe("expandTemplate", () => {
  it("returns pretty-printed JSON when template is empty", () => {
    const obj = { host: "db01", severity: "critical" };
    const result = expandTemplate("", obj);
    expect(result).toBe(JSON.stringify(obj, null, 2));
  });

  it("substitutes known fields using {{field}} syntax", () => {
    const result = expandTemplate("{{host}} ({{severity}})", {
      host: "db01",
      severity: "critical",
    });
    expect(result).toBe("db01 (critical)");
  });

  it("expands unknown fields to empty string", () => {
    const result = expandTemplate("{{nope}}", { host: "db01" });
    expect(result).toBe("");
  });

  it("expands undefined field values to empty string (not 'undefined')", () => {
    const result = expandTemplate("{{host}}", { host: undefined });
    expect(result).toBe("");
  });

  it("expands multiple occurrences of the same field correctly", () => {
    const result = expandTemplate("{{host}} {{host}}", { host: "db01" });
    expect(result).toBe("db01 db01");
  });

  it("handles numeric field values by converting to string", () => {
    const result = expandTemplate("{{count}}", { count: 42 });
    expect(result).toBe("42");
  });

  it("returns unchanged template when no placeholders match", () => {
    const result = expandTemplate("no placeholders here", { host: "db01" });
    expect(result).toBe("no placeholders here");
  });
});
