// Reading and addressing one field's error inside the analysis form.
//
// The resolver in `schema.ts` and the server's 422 both key their messages by
// the SAME json path (`remediation_plan.steps[0].risk`), and `toFieldErrors`
// lifts those into react-hook-form's nested error tree. These two helpers are
// the other half of that contract: `errorAt` reads a message back out by the
// path that produced it, and `toRhfPath` normalises the server's bracket
// notation for `setError`.
//
// Reading by path rather than by typed accessor is deliberate:
// `StepListEditor` renders both `remediation_plan.steps` and
// `remediation_plan.rollback` from one component, so the field it is looking
// up is only known at runtime.
import type { FieldErrors } from "react-hook-form";
import type { AnalysisForm } from "./schema";

/** Split both notations — `a[0].b` and `a.0.b` — into their keys. */
function pathKeys(path: string): string[] {
  return path.split(/[.[\]]+/).filter((k) => k !== "");
}

/**
 * errorAt returns the message stored at `path`, or undefined when that field
 * is currently valid.
 */
export function errorAt(errors: FieldErrors<AnalysisForm>, path: string): string | undefined {
  let node: unknown = errors;
  for (const key of pathKeys(path)) {
    if (node === null || typeof node !== "object") return undefined;
    node = (node as Record<string, unknown>)[key];
  }
  if (node === null || typeof node !== "object") return undefined;
  const message = (node as { message?: unknown }).message;
  return typeof message === "string" && message !== "" ? message : undefined;
}

/**
 * describeFieldError turns a validator fragment into the sentence the field
 * shows.
 *
 * Both validators speak in fragments ("is required", "must be at most 500
 * characters") because the server prefixes them with the json path when it
 * logs them. A field that printed the fragment alone would say "is required"
 * under an empty box; prefixing with the field's own visible label gives
 * "Summary is required." — the same words the person reading the form already
 * has in front of them.
 */
export function describeFieldError(label: string, message: string | undefined): string | undefined {
  if (message === undefined) return undefined;
  const sentence = `${label} ${message}`;
  return /[.!?]$/.test(sentence) ? sentence : `${sentence}.`;
}

/**
 * toRhfPath rewrites a server path into the dot notation `setError` takes.
 * react-hook-form accepts either, but normalising once keeps the error tree
 * keyed identically no matter which side produced the message.
 */
export function toRhfPath(serverPath: string): string {
  return pathKeys(serverPath).join(".");
}

// Which paths this form can actually SHOW a message on.
//
// `setError` accepts any path: it writes into react-hook-form's error tree
// whether or not a control reads that branch back. A server 422 whose path no
// field renders — the body path `$` (trailing data, an unattributable type
// error), `source` (a constant the UI stamps, with no control by design), or a
// key from a server build ahead of this bundle — therefore used to produce a
// form that had silently swallowed the refusal: no red box, no banner, and a
// Save button that looked like it had simply not been pressed. Partitioning on
// this predicate is what lets the editor place what it can and say the rest out
// loud.

const ROOT_CAUSE_FIELDS = new Set(["summary", "detail", "scope", "confidence"]);
/** The two free-text lists, each rendered one input per row. */
const ROOT_CAUSE_LISTS = new Set(["evidence", "caveats"]);
const STEP_LISTS = new Set(["steps", "rollback"]);
const STEP_FIELDS = new Set(["action", "command", "risk"]);
/**
 * `when` has a control on the plan's steps only: a rollback step has no
 * "now" of its own, so the editor offers no timing there, and a server
 * message on `rollback[i].when` must be bannered rather than swallowed.
 */
const PLAN_STEP_FIELDS = new Set([...STEP_FIELDS, "when"]);

function lengthOf(value: unknown): number {
  return Array.isArray(value) ? value.length : 0;
}

/** A path segment that indexes a row this form currently renders. */
function indexIn(key: string | undefined, length: number): boolean {
  if (key === undefined || !/^\d+$/.test(key)) return false;
  const i = Number(key);
  return i < length;
}

function isRootCausePath(keys: string[], form: AnalysisForm): boolean {
  const [field, index, ...tail] = keys;
  if (field === undefined || tail.length > 0) return false;
  if (ROOT_CAUSE_FIELDS.has(field)) return index === undefined;
  if (!ROOT_CAUSE_LISTS.has(field)) return false;
  // The list itself carries the "at most N items" message; a row carries its
  // own, but only while that row exists.
  const rows = field === "evidence" ? form.root_cause.evidence : form.root_cause.caveats;
  return index === undefined || indexIn(index, lengthOf(rows));
}

function isPlanPath(keys: string[], form: AnalysisForm): boolean {
  const [list, index, field, ...tail] = keys;
  if (list === "status") return index === undefined;
  if (list === undefined || !STEP_LISTS.has(list) || tail.length > 0) return false;
  if (index === undefined) return true;
  const rows = list === "steps" ? form.remediation_plan.steps : form.remediation_plan.rollback;
  if (!indexIn(index, lengthOf(rows))) return false;
  // A bare `remediation_plan.steps[0]` addresses a row, not a control: the row
  // renders three fields and no message of its own.
  return field !== undefined && (list === "steps" ? PLAN_STEP_FIELDS : STEP_FIELDS).has(field);
}

/**
 * isAddressablePath answers whether `path` names a control `form` renders, so
 * a message written there with `setError` would actually be read back by
 * {@link errorAt}. `form` is the submitted values, because whether
 * `remediation_plan.steps[7].risk` is addressable depends on how many steps
 * there are.
 */
export function isAddressablePath(path: string, form: AnalysisForm): boolean {
  const [head, ...rest] = pathKeys(path);
  if (head === "root_cause") return isRootCausePath(rest, form);
  if (head === "remediation_plan") return isPlanPath(rest, form);
  return false;
}
