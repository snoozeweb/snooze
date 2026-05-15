// Wire <-> form-state translators for the Modification discriminated union.
//
// The HTTP/storage shape is the legacy positional form ["OP", "field", arg…]
// — same as the Python era and the Go-internal modification.Modification.
// The React editor works in the object form because TypeScript discriminated
// unions are easier to render than positional tuples; this module is the
// single conversion site at the network boundary.
import type { Modification } from "./types";

/** Encode one Modification as the positional wire form. */
export function toWire(m: Modification): unknown[] {
  switch (m.type) {
    case "set":
      return ["SET", m.field, m.value];
    case "delete":
      return ["DELETE", m.field];
    case "array_append":
      return ["ARRAY_APPEND", m.field, m.value];
    case "regex_sub":
      return ["REGEX_SUB", m.field, m.pattern, m.replace];
  }
}

/** Decode a positional wire entry into a Modification, or null if unrecognised. */
export function fromWire(entry: unknown): Modification | null {
  if (!Array.isArray(entry) || entry.length === 0) return null;
  const op = typeof entry[0] === "string" ? entry[0] : "";
  const field = typeof entry[1] === "string" ? entry[1] : "";
  switch (op) {
    case "SET":
      return { type: "set", field, value: stringArg(entry[2]) };
    case "DELETE":
      return { type: "delete", field };
    case "ARRAY_APPEND":
      return { type: "array_append", field, value: stringArg(entry[2]) };
    case "REGEX_SUB":
      return {
        type: "regex_sub",
        field,
        pattern: stringArg(entry[2]),
        replace: stringArg(entry[3]),
      };
    default:
      return null;
  }
}

/** Encode an array of Modifications for transmission. Pure function. */
export function modificationsToWire(mods: readonly Modification[]): unknown[][] {
  return mods.map(toWire);
}

/**
 * Decode an array of modifications received from the API. Unknown entries
 * (KV_SET, REGEX_PARSE, future ops) are dropped from the editor view — they
 * are still preserved on the server because the editor patches `modifications`
 * as a whole field. Callers that need the raw stream should read the server
 * payload directly.
 */
export function modificationsFromWire(raw: unknown): Modification[] {
  if (!Array.isArray(raw)) return [];
  const out: Modification[] = [];
  for (const e of raw) {
    const m = fromWire(e);
    if (m) out.push(m);
  }
  return out;
}

function stringArg(v: unknown): string {
  if (typeof v === "string") return v;
  if (typeof v === "number" || typeof v === "boolean") return String(v);
  return "";
}
