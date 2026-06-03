// Context-aware completion for the search DSL.
//
// Given the text and a cursor position, this module decides whether the
// user is typing a field name, an operator, or a value, and returns a
// ranked list of candidates. The SearchBar component renders those
// candidates in a popover.
//
// Design goals:
//   - Pure function. No DOM, no React. Trivially unit-testable.
//   - Cheap to call on every keystroke. Tokenizes the prefix only, never
//     parses past the cursor.
//   - Resilient to invalid input — the user is mid-keystroke; we don't
//     want a missing closing bracket to silence the suggester.

import { tokenize, type Token } from "./lexer";

export type FieldInfo = {
  name: string;
  type: string;
  description?: string;
  values?: string[];
};

export type SuggestionKind = "field" | "operator" | "value" | "keyword";

export type Suggestion = {
  kind: SuggestionKind;
  /** Text inserted in place of the current token. */
  value: string;
  /** Label shown in the popover. */
  label: string;
  /** Optional secondary text (e.g. operator description). */
  detail?: string;
};

export type SuggestContext = {
  /** Kind of completion appropriate at the cursor. */
  kind: SuggestionKind;
  /** Suggestions, already filtered by the partial token under the cursor. */
  items: Suggestion[];
  /** Byte range to replace when the user picks a suggestion. */
  replaceFrom: number;
  replaceTo: number;
  /**
   * The field name on the LHS of the current comparison, when the cursor is
   * positioned to the right of an operator. Lets the value-suggester pull
   * enum values from the field catalog.
   */
  field?: string;
};

/** Canonical operators offered after a field name. */
export const OPERATORS: { value: string; label: string; detail: string }[] = [
  { value: "=", label: "=", detail: "Equals" },
  { value: "!=", label: "!=", detail: "Not equals" },
  { value: "~", label: "~", detail: "Regex match" },
  { value: "MATCHES", label: "MATCHES", detail: "Regex match (keyword form)" },
  { value: "?", label: "?", detail: "Field exists" },
  { value: "EXISTS", label: "EXISTS", detail: "Field exists (keyword form)" },
  { value: ">", label: ">", detail: "Greater than" },
  { value: ">=", label: ">=", detail: "Greater than or equal" },
  { value: "<", label: "<", detail: "Less than" },
  { value: "<=", label: "<=", detail: "Less than or equal" },
  { value: "CONTAINS", label: "CONTAINS", detail: "List/string contains value" },
  { value: "IN", label: "IN", detail: "Value is in field (reverse contains)" },
];

/** Boolean keywords offered between two terms. */
export const COMBINATORS: { value: string; label: string; detail: string }[] = [
  { value: "AND", label: "AND", detail: "Both terms must match" },
  { value: "OR", label: "OR", detail: "Either term may match" },
  { value: "NOT", label: "NOT", detail: "Negate the next term" },
];

/**
 * Suggest a list of completions for `text` at the given `cursor` byte
 * offset. `fields` is the live catalog from /api/v1/condition/fields.
 *
 * The result's `replaceFrom`/`replaceTo` describe the range a chosen
 * suggestion should overwrite — this lets the SearchBar handle partial
 * tokens uniformly (typing `hos` → suggestion `host` should replace `hos`,
 * not append).
 */
export function suggest(text: string, cursor: number, fields: FieldInfo[]): SuggestContext {
  const tokens = tokenize(text);

  // Find the token whose span contains the cursor; if the cursor is at the
  // boundary between tokens, we attach it to the previous one for context
  // but treat the partial under the cursor as empty.
  const tokenIdx = findTokenAt(tokens, cursor);
  const cur = tokenIdx >= 0 ? tokens[tokenIdx]! : undefined;

  // Determine the partial token currently under the cursor (the prefix we
  // filter suggestions by). When the cursor sits on whitespace, the partial
  // is the empty string.
  let partial = "";
  let replaceFrom = cursor;
  let replaceTo = cursor;
  if (cur && cur.kind !== "eof" && cur.pos < cursor && cursor <= cur.pos + cur.len) {
    if (cur.kind === "ident" || cur.kind === "string" || cur.kind === "number") {
      partial = cur.text.slice(0, cursor - cur.pos);
      replaceFrom = cur.pos;
      replaceTo = cur.pos + cur.len;
    }
  }

  // Determine the completion phase by walking left-to-right through every
  // token that *ends* before the cursor (i.e. excluding the partial under
  // the cursor). The walker is a small state machine; see classifyPhase.
  const phase = classifyPhase(tokens, cursor, replaceFrom);
  const ctx: Context = phase;

  switch (ctx.kind) {
    case "field": {
      const items: Suggestion[] = [];
      for (const f of fields) {
        items.push({
          kind: "field",
          value: f.name,
          label: f.name,
          detail: describeField(f),
        });
      }
      for (const c of COMBINATORS) {
        items.push({ kind: "keyword", value: c.value, label: c.value, detail: c.detail });
      }
      return finalize(items, partial, replaceFrom, replaceTo, "field");
    }
    case "operator": {
      const items: Suggestion[] = OPERATORS.map((o) => ({
        kind: "operator",
        value: o.value,
        label: o.label,
        detail: o.detail,
      }));
      return finalize(items, partial, replaceFrom, replaceTo, "operator");
    }
    case "value": {
      const items: Suggestion[] = [];
      const field = fieldFromPreviousComparison(text, cursor) ?? "";
      const f = field ? fields.find((x) => x.name === field) : undefined;
      if (f?.values) {
        for (const v of f.values) {
          items.push({ kind: "value", value: needsQuoting(v) ? `"${v}"` : v, label: v });
        }
      }
      if (f?.type === "string" && (!f.values || f.values.length === 0)) {
        // Hint for free-form string fields: empty quotes as a starter.
        items.push({ kind: "value", value: '""', label: '"…"', detail: "string" });
      }
      if (f?.type === "number") {
        items.push({ kind: "value", value: "0", label: "0", detail: "number" });
      }
      if (f?.type === "array") {
        items.push({ kind: "value", value: "[]", label: "[…]", detail: "array literal" });
      }
      const result = finalize(items, partial, replaceFrom, replaceTo, "value");
      if (field) result.field = field;
      return result;
    }
    case "keyword": {
      const items: Suggestion[] = COMBINATORS.map((c) => ({
        kind: "keyword",
        value: c.value,
        label: c.value,
        detail: c.detail,
      }));
      return finalize(items, partial, replaceFrom, replaceTo, "keyword");
    }
  }
}

function describeField(f: FieldInfo): string {
  if (f.description) return `${f.type} — ${f.description}`;
  return f.type;
}

/**
 * Quoting policy mirrors the Python/Go parser's "valid_word" rule:
 * unquoted identifiers may contain only [A-Za-z0-9_.-]. Anything else
 * must be wrapped in quotes (we use double).
 */
function needsQuoting(v: string): boolean {
  return !/^[A-Za-z0-9_.-]+$/.test(v);
}

function finalize(
  items: Suggestion[],
  partial: string,
  replaceFrom: number,
  replaceTo: number,
  kind: SuggestionKind,
): SuggestContext {
  const lower = partial.toLowerCase();
  const filtered = lower ? items.filter((s) => s.label.toLowerCase().includes(lower)) : items;
  return { kind, items: filtered, replaceFrom, replaceTo };
}

/**
 * classifyPhase walks left-to-right through every token that ends before
 * the cursor (i.e. excluding the partial under the cursor at
 * `partialStart`, if any) and returns the completion phase expected at
 * the cursor.
 *
 * The state machine has four phases that map 1:1 to SuggestionKind:
 *
 *   start          → field
 *   after_field    → operator
 *   after_op       → value
 *   after_value    → keyword (combinator) or new field
 *
 * Bracketed and quoted literals collapse the phase deterministically: a
 * closing `]` or `}` or quoted string moves us to `after_value`; a `(`
 * pushes a `start` phase that we resume on `)`. NOT and combinators reset
 * to `start`.
 */
function classifyPhase(tokens: Token[], cursor: number, partialStart: number): Context {
  type Phase = "start" | "after_field" | "after_op" | "after_value";
  let phase: Phase = "start";

  for (const t of tokens) {
    if (t.kind === "eof") break;
    const end = t.pos + t.len;
    // Skip the partial under the cursor (its start equals partialStart).
    if (t.pos === partialStart && end >= cursor) break;
    if (end > cursor) break;

    switch (phase) {
      case "start":
        if (t.kind === "ident" || t.kind === "string") {
          phase = "after_field";
        } else if (t.kind === "number" || t.kind === "bool") {
          // A bare literal at start position is a SEARCH term — treat as
          // a value, so what follows is a combinator.
          phase = "after_value";
        } else if (t.kind === "not" || t.kind === "lparen" || t.kind === "comma") {
          phase = "start";
        } else if (t.kind === "lbrack") {
          // Array literal at start — bare SEARCH. We don't try to track
          // nesting; the next combinator/field will reset us anyway.
          phase = "after_value";
        }
        break;
      case "after_field":
        if (
          t.kind === "eq" ||
          t.kind === "neq" ||
          t.kind === "lt" ||
          t.kind === "lte" ||
          t.kind === "gt" ||
          t.kind === "gte" ||
          t.kind === "matches" ||
          t.kind === "contains"
        ) {
          phase = "after_op";
        } else if (t.kind === "exists_sym" || t.kind === "exists_kw") {
          phase = "after_value";
        } else if (t.kind === "in") {
          // RHS of IN is a field name, so we land back in start.
          phase = "start";
        } else if (t.kind === "and" || t.kind === "or") {
          // The previous ident was a bare SEARCH term, and now we have a
          // combinator. Reset to start.
          phase = "start";
        } else if (t.kind === "rparen") {
          // Closed a `(field)` group treated as SEARCH — combinator next.
          phase = "after_value";
        } else if (t.kind === "ident" || t.kind === "string") {
          // Implicit AND: the previous ident was a bare SEARCH. Stay in
          // "after_field" because this new ident is the next term's field.
          phase = "after_field";
        }
        break;
      case "after_op":
        if (
          t.kind === "ident" ||
          t.kind === "string" ||
          t.kind === "number" ||
          t.kind === "bool" ||
          t.kind === "rbrack" ||
          t.kind === "rbrace"
        ) {
          phase = "after_value";
        } else if (t.kind === "lbrack" || t.kind === "lbrace") {
          // Stay in after_op while inside the literal — closing token will
          // bump us to after_value above.
          phase = "after_op";
        }
        break;
      case "after_value":
        if (t.kind === "and" || t.kind === "or" || t.kind === "comma") {
          phase = "start";
        } else if (t.kind === "rparen") {
          phase = "after_value";
        } else if (t.kind === "ident" || t.kind === "string") {
          // Implicit AND between two complete terms.
          phase = "after_field";
        } else if (t.kind === "not") {
          phase = "start";
        }
        break;
    }
  }

  switch (phase) {
    case "start":
      return { kind: "field" };
    case "after_field":
      return { kind: "operator" };
    case "after_op":
      return { kind: "value" };
    case "after_value":
      return { kind: "keyword" };
  }
}

/**
 * findTokenAt returns the index of the token whose span contains `cursor`,
 * or -1. The EOF token is excluded.
 */
function findTokenAt(tokens: Token[], cursor: number): number {
  for (let i = 0; i < tokens.length; i++) {
    const t = tokens[i]!;
    if (t.kind === "eof") continue;
    if (cursor > t.pos && cursor <= t.pos + t.len) return i;
  }
  return -1;
}

type Context = { kind: "field" } | { kind: "operator" } | { kind: "value" } | { kind: "keyword" };

/**
 * fieldFromPreviousComparison walks the token stream backwards from the
 * cursor, looking for the most recent `ident OP` pair. Returns the field
 * name when found, undefined otherwise. Used by the suggester to populate
 * `SuggestContext.field` for value completion.
 */
export function fieldFromPreviousComparison(text: string, cursor: number): string | undefined {
  const toks = tokenize(text);
  // Find the operator immediately before the cursor (or its position).
  let opIdx = -1;
  for (let i = toks.length - 1; i >= 0; i--) {
    const t = toks[i]!;
    if (t.kind === "eof") continue;
    if (t.pos + t.len > cursor) continue;
    if (
      t.kind === "eq" ||
      t.kind === "neq" ||
      t.kind === "lt" ||
      t.kind === "lte" ||
      t.kind === "gt" ||
      t.kind === "gte" ||
      t.kind === "matches" ||
      t.kind === "contains"
    ) {
      opIdx = i;
      break;
    }
    // Any literal / paren / combinator before the op means we've already
    // closed the previous comparison.
    if (t.kind === "ident" || t.kind === "string" || t.kind === "number" || t.kind === "bool")
      continue;
    if (
      t.kind === "lparen" ||
      t.kind === "rparen" ||
      t.kind === "and" ||
      t.kind === "or" ||
      t.kind === "not" ||
      t.kind === "comma"
    ) {
      return undefined;
    }
  }
  if (opIdx <= 0) return undefined;
  const left = toks[opIdx - 1];
  if (!left) return undefined;
  if (left.kind === "ident") return left.text;
  if (left.kind === "string") return left.value ?? left.text;
  return undefined;
}
