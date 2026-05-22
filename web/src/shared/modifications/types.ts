// The discriminated union the editor works in. Round-trips with the
// positional wire shape via wire.ts.
//
// The Go backend (internal/modification.modification.go + the rule plugin's
// special-cased KV_SET) recognises these ops:
//
//   SET, DELETE, ARRAY_APPEND, ARRAY_DELETE, REGEX_PARSE, REGEX_SUB, KV_SET
//
// All seven are editable in the UI; unknown ops still drop through.
export type Modification =
  | { type: "set"; field: string; value: string }
  | { type: "delete"; field: string }
  | { type: "array_append"; field: string; value: string }
  | { type: "array_delete"; field: string; value: string }
  | { type: "regex_parse"; field: string; pattern: string }
  | { type: "regex_sub"; field: string; pattern: string; replace: string }
  | { type: "kv_set"; field: string; key: string };

export type ModificationType = Modification["type"];

export const MODIFICATION_TYPES: ReadonlyArray<{ value: ModificationType; label: string }> = [
  { value: "set", label: "Set" },
  { value: "delete", label: "Delete" },
  { value: "array_append", label: "Array append" },
  { value: "array_delete", label: "Array delete" },
  { value: "regex_parse", label: "Regex parse" },
  { value: "regex_sub", label: "Regex substitute" },
  { value: "kv_set", label: "KV set" },
];

export function defaultModification(type: ModificationType): Modification {
  switch (type) {
    case "delete":
      return { type: "delete", field: "" };
    case "array_append":
      return { type: "array_append", field: "", value: "" };
    case "array_delete":
      return { type: "array_delete", field: "", value: "" };
    case "regex_parse":
      return { type: "regex_parse", field: "", pattern: "" };
    case "regex_sub":
      return { type: "regex_sub", field: "", pattern: "", replace: "" };
    case "kv_set":
      return { type: "kv_set", field: "", key: "" };
    case "set":
      return { type: "set", field: "", value: "" };
  }
}
