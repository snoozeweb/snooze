import { describe, expect, it } from "vitest";
import { encodeConditionQ, type Condition } from "./serialize";

function b64urlDecode(s: string): string {
  const padded = s + "=".repeat((4 - (s.length % 4)) % 4);
  return atob(padded.replace(/-/g, "+").replace(/_/g, "/"));
}

describe("encodeConditionQ", () => {
  it("encodes a simple EQUALS leaf", () => {
    const c: Condition = { type: "EQUALS", field: "record_uid", value: "abc" };
    const q = encodeConditionQ(c);
    expect(typeof q).toBe("string");
    expect(JSON.parse(b64urlDecode(q))).toEqual(c);
  });

  it("encodes an AND of two leaves", () => {
    const c: Condition = {
      type: "AND",
      args: [
        { type: "EQUALS", field: "host", value: "srv-1" },
        { type: "EQUALS", field: "severity", value: "critical" },
      ],
    };
    expect(JSON.parse(b64urlDecode(encodeConditionQ(c)))).toEqual(c);
  });

  it("uses URL-safe base64 (no +, /, or =)", () => {
    const c: Condition = { type: "EQUALS", field: "f", value: "????>>" };
    const q = encodeConditionQ(c);
    expect(q).not.toMatch(/[+/=]/);
  });
});
