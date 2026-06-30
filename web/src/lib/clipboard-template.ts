/**
 * Expand a clipboard template against a Record_-shaped object.
 * `{{field}}` is replaced by the field's string representation. Primitive
 * values (string, number, boolean) are converted directly; `null`, `undefined`,
 * and unknown fields expand to "". Object/array values are JSON-serialised.
 * Returns pretty-printed JSON of the object when template is empty (preserving
 * the default copy-as-JSON behaviour).
 */
export function expandTemplate(template: string, obj: Record<string, unknown>): string {
  if (!template) return JSON.stringify(obj, null, 2);
  return template.replace(/\{\{(\w+)\}\}/g, (_, key: string) => {
    const val = obj[key];
    if (val === null || val === undefined) return "";
    if (typeof val === "string" || typeof val === "number" || typeof val === "boolean") {
      return String(val);
    }
    // Fallback: JSON-serialise objects/arrays rather than "[object Object]"
    return JSON.stringify(val);
  });
}
