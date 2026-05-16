import type { Rule } from "./types";

export function ruleRowDisabled(r: Rule): boolean {
  return r.enabled === false;
}
