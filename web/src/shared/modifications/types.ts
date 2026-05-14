export type Modification =
  | { type: "set"; field: string; value: string }
  | { type: "delete"; field: string }
  | { type: "regex_sub"; field: string; pattern: string; replace: string }
  | { type: "array_append"; field: string; value: string };

export type ModificationType = Modification["type"];

export const MODIFICATION_TYPES: ReadonlyArray<{ value: ModificationType; label: string }> = [
  { value: "set", label: "Set" },
  { value: "delete", label: "Delete" },
  { value: "regex_sub", label: "Regex substitute" },
  { value: "array_append", label: "Array append" },
];

export function defaultModification(type: ModificationType): Modification {
  if (type === "delete") return { type: "delete", field: "" };
  if (type === "regex_sub") return { type: "regex_sub", field: "", pattern: "", replace: "" };
  if (type === "array_append") return { type: "array_append", field: "", value: "" };
  return { type: "set", field: "", value: "" };
}
