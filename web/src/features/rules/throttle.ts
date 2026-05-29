// Throttle wire (de)serialization for aggregate rules.
//
// Wire shape mirrors internal/pluginimpl/aggregaterule: `throttle` is either a
// scalar number (seconds, applies always) or a { value: seconds, …, default:
// seconds } map matched against the rule's `watch` field values.
//
// The editor works with a flattened form — a default plus an ordered list of
// {value, seconds} overrides — and converts at the wire boundary.

export type ThrottleOverride = { value: string; seconds: number };
export type ThrottleForm = { defaultSeconds: number; overrides: ThrottleOverride[] };

const DEFAULT_KEY = "default";

export function throttleFromWire(wire: number | Record<string, number> | undefined): ThrottleForm {
  if (wire === undefined || wire === null) {
    return { defaultSeconds: 0, overrides: [] };
  }
  if (typeof wire === "number") {
    return { defaultSeconds: wire, overrides: [] };
  }
  const overrides: ThrottleOverride[] = [];
  let defaultSeconds = 0;
  for (const [value, seconds] of Object.entries(wire)) {
    if (value === DEFAULT_KEY) {
      defaultSeconds = seconds;
      continue;
    }
    overrides.push({ value, seconds });
  }
  overrides.sort((a, b) => a.value.localeCompare(b.value));
  return { defaultSeconds, overrides };
}

export function throttleToWire(form: ThrottleForm): number | Record<string, number> {
  const rows = form.overrides.filter((o) => o.value.trim() !== "");
  if (rows.length === 0) {
    return form.defaultSeconds;
  }
  const out: Record<string, number> = {};
  for (const o of rows) {
    out[o.value.trim()] = o.seconds;
  }
  out[DEFAULT_KEY] = form.defaultSeconds;
  return out;
}
