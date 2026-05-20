import { describe, expect, it } from "vitest";
import { suggest, type FieldInfo } from "./suggest";

const FIELDS: FieldInfo[] = [
  { name: "host", type: "string", description: "Host" },
  {
    name: "severity",
    type: "string",
    values: ["critical", "warning", "info"],
  },
  { name: "state", type: "string", values: ["open", "ack", "close"] },
  { name: "date_epoch", type: "number" },
];

describe("searchdsl suggest", () => {
  it("offers field names at the start of input", () => {
    const ctx = suggest("", 0, FIELDS);
    expect(ctx.kind).toBe("field");
    const names = ctx.items.map((s) => s.value);
    expect(names).toContain("host");
    expect(names).toContain("severity");
    // Combinators are also included so an AND-led implicit group is possible.
    expect(names).toContain("AND");
  });

  it("filters field suggestions by the partial under the cursor", () => {
    const text = "sev";
    const ctx = suggest(text, text.length, FIELDS);
    expect(ctx.kind).toBe("field");
    expect(ctx.items.map((s) => s.value)).toContain("severity");
    // Replace range covers the whole "sev" token.
    expect(ctx.replaceFrom).toBe(0);
    expect(ctx.replaceTo).toBe(3);
  });

  it("offers operators after a field name", () => {
    const text = "host ";
    const ctx = suggest(text, text.length, FIELDS);
    expect(ctx.kind).toBe("operator");
    expect(ctx.items.map((s) => s.value)).toEqual(
      expect.arrayContaining(["=", "!=", "~", "MATCHES", "?", ">", "<", "CONTAINS", "IN"]),
    );
  });

  it("offers enum values after `severity =`", () => {
    const text = "severity = ";
    const ctx = suggest(text, text.length, FIELDS);
    expect(ctx.kind).toBe("value");
    expect(ctx.field).toBe("severity");
    expect(ctx.items.map((s) => s.label)).toEqual(["critical", "warning", "info"]);
  });

  it("offers enum values for state and filters by partial", () => {
    const text = "state = ac";
    const ctx = suggest(text, text.length, FIELDS);
    expect(ctx.kind).toBe("value");
    expect(ctx.field).toBe("state");
    expect(ctx.items.map((s) => s.label)).toEqual(["ack"]);
    expect(ctx.replaceFrom).toBe("state = ".length);
    expect(ctx.replaceTo).toBe("state = ac".length);
  });

  it("offers AND/OR after a closed comparison", () => {
    const text = "severity = critical ";
    const ctx = suggest(text, text.length, FIELDS);
    expect(ctx.kind).toBe("keyword");
    const values = ctx.items.map((s) => s.value);
    expect(values).toEqual(expect.arrayContaining(["AND", "OR"]));
  });

  it("offers field suggestions after AND", () => {
    const text = "severity = critical AND ";
    const ctx = suggest(text, text.length, FIELDS);
    expect(ctx.kind).toBe("field");
    expect(ctx.items.map((s) => s.value)).toContain("host");
  });

  it("offers value suggestions after MATCHES", () => {
    const text = `message MATCHES `;
    const ctx = suggest(text, text.length, FIELDS);
    expect(ctx.kind).toBe("value");
  });

  it("quotes value when the enum value would not parse as an identifier", () => {
    const fields: FieldInfo[] = [
      { name: "status", type: "string", values: ["needs review", "ok"] },
    ];
    const text = "status = ";
    const ctx = suggest(text, text.length, fields);
    const review = ctx.items.find((s) => s.label === "needs review");
    expect(review?.value).toBe('"needs review"');
  });
});
