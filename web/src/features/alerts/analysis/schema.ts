// Client-side mirror of the server's agentic-analysis validator.
//
// The authority is `pkg/snoozetypes/agentic.go`; this file exists so a typo is
// caught before the round trip, not so the server can stop checking. Two
// things are therefore copied VERBATIM rather than paraphrased:
//
//   - the error PATHS (`root_cause.evidence[2]`,
//     `remediation_plan.steps[0].risk`). The server reports a 422 as
//     `error.details` keyed by exactly these strings, so an editor can drop
//     server failures into the same react-hook-form error tree with
//     `setError(path, …)` and have them land on the same fields the local
//     resolver uses. Any drift and a server error would render nowhere.
//   - the error MESSAGES, so a rule does not appear to change its mind
//     depending on which side caught it.
//
// Lengths are counted in CHARACTERS, matching the server's
// `utf8.RuneCountInString`. JavaScript's `String.length` counts UTF-16 code
// units, which would reject a 300-emoji summary as "over 500" — a limit
// nobody could reason about. `Array.from` iterates code points.
import type { FieldErrors, Resolver } from "react-hook-form";
import type { components } from "@/lib/api/types.gen";
import { CONFIDENCE_LEVELS, RISK_LEVELS, isConfidence, isRisk } from "./enums";
import type { Confidence, Risk } from "./enums";

type Agentic = components["schemas"]["Agentic"];
type AgenticRequest = components["schemas"]["AgenticRequest"];
type AgenticStep = components["schemas"]["AgenticStep"];

/** Size limits, mirroring the `Max*` constants in pkg/snoozetypes/agentic.go. */
export const ANALYSIS_LIMITS = {
  summary: 500,
  scope: 200,
  evidenceItems: 10,
  evidence: 500,
  steps: 20,
  action: 500,
  command: 1000,
  source: 64,
} as const;

/** The `source` tag the web UI stamps on everything it writes. */
export const ANALYSIS_SOURCE = "snooze-web";

// Rendered enum lists, reused inside messages exactly as the server renders
// them, so a reader gets the accepted values off the error itself.
const CONFIDENCE_ENUM = CONFIDENCE_LEVELS.join("|");
const RISK_ENUM = RISK_LEVELS.join("|");

const NUL_MESSAGE = "must not contain the NUL character";

/**
 * The code points Go's `unicode.IsSpace` calls space — the set
 * `strings.TrimSpace` strips, and therefore the set the server's
 * required-ness checks measure a field against.
 *
 * It is deliberately NOT `String.prototype.trim`'s set. The two disagree on
 * exactly two characters, and both reach a text field by paste: U+0085 (NEL)
 * is blank to Go and not to JS, U+FEFF (ZWNBSP) is blank to JS and not to Go.
 * Either disagreement produces a summary one validator calls required and the
 * other calls fine — a save the form says is good and the server rejects, or
 * the reverse.
 */
const GO_SPACE = /^[\t\n\v\f\r \u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]*$/;

/** isBlank mirrors the server's `strings.TrimSpace(s) == ""`. */
export function isBlank(s: string): boolean {
  return GO_SPACE.test(s);
}

// The three narrowings below exist because the agentic subtree is stored as
// loose JSON on a dynamic record: it can be hand-edited in the DB, written by
// an older server, or carry a type a future one allows. A `summary` that is a
// number used to throw inside the resolver, and there is no ErrorBoundary in
// this SPA — a throw here takes down the whole /alerts route, not one pane.

function asText(value: unknown): string {
  return typeof value === "string" ? value : "";
}

function asList(value: unknown): unknown[] {
  return Array.isArray(value) ? value : [];
}

function asRecord(value: unknown): Record<string, unknown> {
  return typeof value === "object" && value !== null ? (value as Record<string, unknown>) : {};
}

/**
 * One step, as the form holds it. `risk` widens to `""` because a fresh step
 * has no answer yet and an unset select must be a validation failure, not a
 * silent default to "low".
 */
export type AnalysisStepForm = {
  action: string;
  command: string;
  risk: Risk | "";
};

/**
 * The editor's form shape. It mirrors `AgenticRequest` one-for-one except
 * that optional server fields are always present (empty string / empty array)
 * — a controlled input cannot hold `undefined` — and `source` is not here: it
 * is a constant the UI stamps, not something a human types.
 */
export type AnalysisForm = {
  root_cause: {
    summary: string;
    scope: string;
    evidence: string[];
    confidence: Confidence | "";
  };
  remediation_plan: {
    steps: AnalysisStepForm[];
    rollback: AnalysisStepForm[];
    automatable: boolean;
  };
};

/**
 * Optional resolver context. Pass `{ source }` to `useForm` when the caller
 * stamps something other than {@link ANALYSIS_SOURCE}; the default is no
 * source at all, which is always valid.
 */
export type AnalysisFormContext = { source?: string };

/** Character count, matching the server's rune count. */
export function runeLength(s: string): number {
  return Array.from(s).length;
}

function tooLong(limit: number): string {
  return `must be at most ${limit} characters`;
}

/**
 * checkNUL mirrors the server's guard. PostgreSQL jsonb cannot store \u0000,
 * so a payload carrying one would validate and then fail at the write on one
 * backend out of three.
 */
function checkNUL(errs: Record<string, string>, path: string, s: string): void {
  if (s.includes("\u0000")) errs[path] = NUL_MESSAGE;
}

function validateStepList(errs: Record<string, string>, prefix: string, steps: unknown[]): void {
  // The server returns early here: an over-long list is reported on the list
  // itself and its members are not walked. Mirrored so the two never disagree
  // about how many errors one payload produces.
  if (steps.length > ANALYSIS_LIMITS.steps) {
    errs[prefix] = `must hold at most ${ANALYSIS_LIMITS.steps} steps`;
    return;
  }
  steps.forEach((raw, i) => {
    const step = asRecord(raw);
    const path = `${prefix}[${i}]`;
    const action = asText(step["action"]);
    const command = asText(step["command"]);
    if (isBlank(action)) {
      errs[`${path}.action`] = "is required";
    } else if (runeLength(action) > ANALYSIS_LIMITS.action) {
      errs[`${path}.action`] = tooLong(ANALYSIS_LIMITS.action);
    }
    checkNUL(errs, `${path}.action`, action);
    if (runeLength(command) > ANALYSIS_LIMITS.command) {
      errs[`${path}.command`] = tooLong(ANALYSIS_LIMITS.command);
    }
    checkNUL(errs, `${path}.command`, command);
    const risk = step["risk"];
    if (risk === "" || risk === undefined) {
      errs[`${path}.risk`] = `is required (${RISK_ENUM})`;
    } else if (!isRisk(risk)) {
      errs[`${path}.risk`] = `must be one of ${RISK_ENUM}`;
    }
  });
}

/**
 * validateAnalysisForm returns every failure at once — a human fixing a form
 * should need one pass, not five — as a flat map from the server's JSON path
 * to its message. That is the same shape a 422's `error.details` carries, so
 * local and remote failures merge without translation.
 *
 * An empty map means valid.
 */
export function validateAnalysisForm(form: AnalysisForm, source?: string): Record<string, string> {
  const errs: Record<string, string> = {};
  const rc = asRecord(asRecord(form)["root_cause"]);
  const summary = asText(rc["summary"]);
  const scope = asText(rc["scope"]);

  if (isBlank(summary)) {
    errs["root_cause.summary"] = "is required";
  } else if (runeLength(summary) > ANALYSIS_LIMITS.summary) {
    errs["root_cause.summary"] = tooLong(ANALYSIS_LIMITS.summary);
  }
  checkNUL(errs, "root_cause.summary", summary);

  if (runeLength(scope) > ANALYSIS_LIMITS.scope) {
    errs["root_cause.scope"] = tooLong(ANALYSIS_LIMITS.scope);
  }
  checkNUL(errs, "root_cause.scope", scope);

  const confidence = rc["confidence"];
  if (confidence === "" || confidence === undefined) {
    errs["root_cause.confidence"] = `is required (${CONFIDENCE_ENUM})`;
  } else if (!isConfidence(confidence)) {
    errs["root_cause.confidence"] = `must be one of ${CONFIDENCE_ENUM}`;
  }

  const evidence = asList(rc["evidence"]);
  if (evidence.length > ANALYSIS_LIMITS.evidenceItems) {
    errs["root_cause.evidence"] = `must hold at most ${ANALYSIS_LIMITS.evidenceItems} items`;
  }
  // Unlike the step lists, the server keeps walking the items after flagging
  // an over-long evidence list, so a caller sees the blank item too.
  evidence.forEach((raw, i) => {
    const ev = asText(raw);
    const path = `root_cause.evidence[${i}]`;
    if (isBlank(ev)) {
      errs[path] = "must not be empty";
      return;
    }
    if (runeLength(ev) > ANALYSIS_LIMITS.evidence) {
      errs[path] = tooLong(ANALYSIS_LIMITS.evidence);
    }
    checkNUL(errs, path, ev);
  });

  const plan = asRecord(asRecord(form)["remediation_plan"]);
  const steps = asList(plan["steps"]);
  if (steps.length === 0) {
    errs["remediation_plan.steps"] = "must hold at least one step";
  }
  validateStepList(errs, "remediation_plan.steps", steps);
  validateStepList(errs, "remediation_plan.rollback", asList(plan["rollback"]));

  if (source !== undefined) {
    const tag = asText(source);
    if (runeLength(tag) > ANALYSIS_LIMITS.source) {
      errs["source"] = tooLong(ANALYSIS_LIMITS.source);
    }
    checkNUL(errs, "source", tag);
  }
  return errs;
}

/** Path segments that reach a prototype instead of a field. Never walked. */
const UNWALKABLE = new Set(["__proto__", "constructor", "prototype"]);

/**
 * setErrorAtPath writes `message` into a react-hook-form error tree at a
 * server path. It accepts both notations (`a[0].b` and `a.0.b`) because the
 * server writes brackets and react-hook-form's own `setError` accepts either.
 */
function setErrorAtPath(tree: Record<string, unknown>, path: string, message: string): void {
  const keys = path.split(/[.[\]]+/).filter((k) => k !== "");
  // `toFieldErrors` is also fed a server 422 whose `details` keys echo names a
  // caller can influence, and `node["__proto__"] = {}` on a plain object walks
  // ONTO Object.prototype rather than creating a property — a single
  // `__proto__.x` path would then write onto every object in the tab. These
  // three segments address no field, so dropping the whole path loses nothing.
  if (keys.length === 0 || keys.some((key) => UNWALKABLE.has(key))) return;
  let node: Record<string, unknown> = tree;
  keys.forEach((key, i) => {
    if (i === keys.length - 1) {
      node[key] = { type: "validate", message };
      return;
    }
    const next = node[key];
    if (next === undefined || typeof next !== "object" || next === null) {
      // Arrays and objects both read back fine as plain objects here —
      // react-hook-form indexes errors by key, not by Array.isArray.
      node[key] = {};
    }
    node = node[key] as Record<string, unknown>;
  });
}

/**
 * toFieldErrors lifts the flat path→message map into the nested tree
 * react-hook-form renders from. Exported because an editor merging a server
 * 422 wants the same lift.
 */
export function toFieldErrors(flat: Record<string, string>): FieldErrors<AnalysisForm> {
  const tree: Record<string, unknown> = {};
  for (const [path, message] of Object.entries(flat)) {
    setErrorAtPath(tree, path, message);
  }
  return tree as FieldErrors<AnalysisForm>;
}

/**
 * analysisResolver is the react-hook-form resolver for the analysis editor.
 * There is no zod in this SPA (see package.json) — validation is a plain
 * function, which is also what lets the paths match the server's byte for
 * byte.
 */
export const analysisResolver: Resolver<AnalysisForm, AnalysisFormContext> = (values, context) => {
  const flat = validateAnalysisForm(values, context?.source);
  if (Object.keys(flat).length > 0) {
    return { values: {}, errors: toFieldErrors(flat) };
  }
  return { values, errors: {} };
};

/** A blank step, as "Add step" appends it. */
export function emptyAnalysisStep(): AnalysisStepForm {
  return { action: "", command: "", risk: "" };
}

/**
 * emptyAnalysisForm is the starting point for authoring from scratch: one
 * blank step (the server requires at least one, so offering zero would open
 * the form already invalid in a way the user cannot see), and an unset
 * confidence — `""` rather than a pre-picked "low", so the resolver makes the
 * author state a confidence instead of inheriting a guess.
 */
export function emptyAnalysisForm(): AnalysisForm {
  return {
    root_cause: { summary: "", scope: "", evidence: [], confidence: "" },
    remediation_plan: { steps: [emptyAnalysisStep()], rollback: [], automatable: false },
  };
}

function stepToForm(raw: unknown): AnalysisStepForm {
  const step = asRecord(raw);
  const risk = step["risk"];
  return {
    action: asText(step["action"]),
    command: asText(step["command"]),
    risk: isRisk(risk) ? risk : "",
  };
}

/**
 * analysisToForm seeds the editor from a stored analysis.
 *
 * Every optional server field is widened to its empty form value, and an
 * out-of-range enum (an older analysis written before a value was retired, or
 * one hand-edited in the DB) becomes `""` rather than being silently kept —
 * the author is then asked to pick a real one before saving.
 */
export function analysisToForm(agentic: Agentic | null | undefined): AnalysisForm {
  const rc = asRecord(asRecord(agentic)["root_cause"]);
  const plan = asRecord(asRecord(agentic)["remediation_plan"]);
  const steps = asList(plan["steps"]).map(stepToForm);
  const confidence = rc["confidence"];
  return {
    root_cause: {
      summary: asText(rc["summary"]),
      scope: asText(rc["scope"]),
      evidence: asList(rc["evidence"]).map(asText),
      confidence: isConfidence(confidence) ? confidence : "",
    },
    remediation_plan: {
      // An analysis stored with no steps cannot exist through the API, but a
      // form with no steps cannot be edited, so fall back to one blank row.
      steps: steps.length > 0 ? steps : [emptyAnalysisStep()],
      rollback: asList(plan["rollback"]).map(stepToForm),
      automatable: plan["automatable"] === true,
    },
  };
}

function stepToRequest(raw: unknown): AgenticStep {
  const step = asRecord(raw);
  const action = asText(step["action"]);
  const command = asText(step["command"]);
  const risk = step["risk"];
  return {
    action,
    // Optional fields are omitted rather than sent empty: the PUT replaces the
    // whole subtree, and `"command": ""` would store a key that reads as "there
    // is a command" to every consumer walking the JSON.
    ...(isBlank(command) ? {} : { command }),
    // The resolver has already rejected "", so the fallback is unreachable on
    // a validated form — it is here to narrow `Risk | ""` without an assertion.
    risk: isRisk(risk) ? risk : "low",
  };
}

/**
 * formToRequest lowers a validated form into the PUT body. Empty optionals are
 * dropped so the stored subtree carries only what the author actually wrote.
 */
export function formToRequest(form: AnalysisForm, source?: string): AgenticRequest {
  const rc = asRecord(asRecord(form)["root_cause"]);
  const plan = asRecord(asRecord(form)["remediation_plan"]);
  const scope = asText(rc["scope"]);
  const evidence = asList(rc["evidence"]).map(asText);
  const rollback = asList(plan["rollback"]).map(stepToRequest);
  const confidence = rc["confidence"];
  const tag = asText(source);
  return {
    root_cause: {
      summary: asText(rc["summary"]),
      ...(isBlank(scope) ? {} : { scope }),
      ...(evidence.length === 0 ? {} : { evidence }),
      // Same narrowing as `risk` above: unreachable on a validated form.
      confidence: isConfidence(confidence) ? confidence : "low",
    },
    remediation_plan: {
      steps: asList(plan["steps"]).map(stepToRequest),
      ...(rollback.length === 0 ? {} : { rollback }),
      // `automatable: false` is the server's zero value and is omitted from
      // the stored JSON anyway, so sending it would only add noise.
      ...(plan["automatable"] === true ? { automatable: true } : {}),
    },
    ...(source !== undefined && !isBlank(tag) ? { source: tag } : {}),
  };
}
