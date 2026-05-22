// Wire <-> form-state translators for the Modification discriminated union.
//
// The HTTP/storage shape is the legacy positional form ["OP", "field", arg…]
// — same as the Python era and the Go-internal modification.Modification.
// The React editor works in the object form because TypeScript discriminated
// unions are easier to render than positional tuples; this module is the
// single conversion site at the network boundary.
//
// All ops the Go backend understands (internal/modification.modification.go +
// the rule plugin's special-cased KV_SET) are decoded here so editing a rule
// that uses one of them round-trips cleanly instead of silently dropping it.
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
    case "array_delete":
      return ["ARRAY_DELETE", m.field, m.value];
    case "regex_parse":
      return ["REGEX_PARSE", m.field, m.pattern];
    case "regex_sub":
      // Python form is ["REGEX_SUB", src_field, dst_field, pattern, replace];
      // when src and dst are the same (the common case) we still emit both so
      // the round-trip with the Go side stays positional.
      return ["REGEX_SUB", m.field, m.field, m.pattern, m.replace];
    case "kv_set":
      return ["KV_SET", m.field, m.key];
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
    case "ARRAY_DELETE":
      return { type: "array_delete", field, value: stringArg(entry[2]) };
    case "REGEX_PARSE":
      return { type: "regex_parse", field, pattern: stringArg(entry[2]) };
    case "REGEX_SUB": {
      // Python emits ["REGEX_SUB", src_field, dst_field, pattern, replace].
      // The editor only models a single field; when src != dst we keep the
      // dst (output) field, matching the user-visible "where the substitution
      // lands" semantic.
      const dst = typeof entry[2] === "string" ? entry[2] : field;
      return {
        type: "regex_sub",
        field: dst || field,
        pattern: stringArg(entry[3]),
        replace: stringArg(entry[4]),
      };
    }
    case "KV_SET":
      return { type: "kv_set", field, key: stringArg(entry[2]) };
    default:
      return null;
  }
}

/** Encode an array of Modifications for transmission. Pure function. */
export function modificationsToWire(mods: readonly Modification[]): unknown[][] {
  return mods.map(toWire);
}

/**
 * Decode an array of modifications received from the API. Entries the editor
 * cannot model (unknown future ops, malformed payloads) are dropped from the
 * editor view — they are still preserved on the server because the editor
 * patches `modifications` as a whole field only when the user intentionally
 * saves; readers that need the raw stream should consume the server payload
 * directly.
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
