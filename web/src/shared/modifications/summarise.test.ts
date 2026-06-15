import { describe, expect, it } from "vitest";
import { summariseModification } from "./summarise";

describe("summariseModification", () => {
  it("renders SET with field and value", () => {
    expect(summariseModification(["SET", "environment", "prod"])).toBe("SET environment = prod");
  });

  it("renders DELETE with just the field", () => {
    expect(summariseModification(["DELETE", "host"])).toBe("DELETE host");
  });

  it("renders ARRAY_APPEND / ARRAY_DELETE with the operator glyph", () => {
    expect(summariseModification(["ARRAY_APPEND", "tags", "urgent"])).toBe(
      "ARRAY_APPEND tags += urgent",
    );
    expect(summariseModification(["ARRAY_DELETE", "tags", "stale"])).toBe(
      "ARRAY_DELETE tags -= stale",
    );
  });

  it("renders REGEX_PARSE field ~ pattern", () => {
    expect(summariseModification(["REGEX_PARSE", "message", "(?P<code>\\d+)"])).toBe(
      "REGEX_PARSE message ~ (?P<code>\\d+)",
    );
  });

  it("renders REGEX_SUB using the destination field and s/// form", () => {
    expect(summariseModification(["REGEX_SUB", "msg", "msg", "foo", "bar"])).toBe(
      "REGEX_SUB msg = s/foo/bar/",
    );
  });

  it("renders KV_SET as out_field = dict[key] for the 4-tuple", () => {
    expect(summariseModification(["KV_SET", "owners", "host", "owner"])).toBe(
      "KV_SET owner = owners[host]",
    );
  });

  it("renders the legacy 3-tuple KV_SET without a dict", () => {
    expect(summariseModification(["KV_SET", "owner", "host"])).toBe("KV_SET owner[host]");
  });

  it("coerces non-string args to strings", () => {
    expect(summariseModification(["SET", "ttl", 3600])).toBe("SET ttl = 3600");
  });

  it("falls back to op + args for an unknown op", () => {
    expect(summariseModification(["FUTURE_OP", "a", "b"])).toBe("FUTURE_OP a b");
  });

  it("returns empty string for an empty or non-array entry", () => {
    expect(summariseModification([])).toBe("");
    expect(summariseModification(null)).toBe("");
    expect(summariseModification("SET")).toBe("");
  });
});
