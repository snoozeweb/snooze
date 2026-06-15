// Human-readable one-line summary of a modification in its positional wire
// form (["OP", "field", arg…] — see wire.ts). Used by read-only views like
// the rules tree's "Modifications" column, where the old rendering showed
// only the op + field ("SET environment") and dropped the value. This keeps
// the full intent visible ("SET environment = prod") without opening the
// editor.
//
// Operates on the raw wire tuple (not the editor's discriminated union) so it
// can render straight off `rule.modifications` without a fromWire() round-trip
// — and so unrecognised/legacy ops still render something useful instead of
// vanishing.

/** Coerce one positional argument to a display string. Mirrors wire.ts. */
function argAt(entry: unknown[], i: number): string {
  const v = entry[i];
  if (typeof v === "string") return v;
  if (typeof v === "number" || typeof v === "boolean") return String(v);
  if (v === undefined || v === null) return "";
  // Args are primitives in practice; JSON-encode the rare object/array rather
  // than emitting "[object Object]".
  return JSON.stringify(v) ?? "";
}

/**
 * Render one modification wire tuple as a compact, readable label.
 * Returns "" for an empty/non-array entry.
 */
export function summariseModification(entry: unknown): string {
  if (!Array.isArray(entry) || entry.length === 0) return "";
  const op = typeof entry[0] === "string" ? entry[0] : String(entry[0] ?? "");
  switch (op) {
    case "SET":
      return `SET ${argAt(entry, 1)} = ${argAt(entry, 2)}`;
    case "DELETE":
      return `DELETE ${argAt(entry, 1)}`;
    case "ARRAY_APPEND":
      return `ARRAY_APPEND ${argAt(entry, 1)} += ${argAt(entry, 2)}`;
    case "ARRAY_DELETE":
      return `ARRAY_DELETE ${argAt(entry, 1)} -= ${argAt(entry, 2)}`;
    case "REGEX_PARSE":
      return `REGEX_PARSE ${argAt(entry, 1)} ~ ${argAt(entry, 2)}`;
    case "REGEX_SUB": {
      // ["REGEX_SUB", src_field, dst_field, pattern, replace] — dst is where
      // the substitution lands; fall back to src when only one field is set.
      const dst = argAt(entry, 2) || argAt(entry, 1);
      return `REGEX_SUB ${dst} = s/${argAt(entry, 3)}/${argAt(entry, 4)}/`;
    }
    case "KV_SET": {
      // Canonical ["KV_SET", dict, key, out_field]; legacy 3-tuple has no dict.
      if (entry.length >= 4) {
        return `KV_SET ${argAt(entry, 3)} = ${argAt(entry, 1)}[${argAt(entry, 2)}]`;
      }
      return `KV_SET ${argAt(entry, 1)}[${argAt(entry, 2)}]`;
    }
    default: {
      // Unknown / future op: show the op verb plus whatever args came with it.
      const rest = entry
        .slice(1)
        .map((_, i) => argAt(entry, i + 1))
        .filter(Boolean)
        .join(" ");
      return rest ? `${op} ${rest}` : op;
    }
  }
}
