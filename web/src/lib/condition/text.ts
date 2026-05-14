// web/src/lib/condition/text.ts
import type { Condition, ConditionType } from "./types";

export type TextParseError = { message: string; pos: number };
export type TextParseResult = { ok: true; value: Condition } | { ok: false; error: TextParseError };

const IDENT_RE = /^[A-Za-z_][A-Za-z0-9_.-]*$/;

function quoteString(s: string): string {
  return `"${s.replace(/\\/g, "\\\\").replace(/"/g, '\\"').replace(/\n/g, "\\n").replace(/\t/g, "\\t")}"`;
}

function encodeIdent(s: string): string {
  return IDENT_RE.test(s) ? s : quoteString(s);
}

function encodeValue(v: string | number | boolean): string {
  if (typeof v === "number") return Number.isInteger(v) ? v.toString() : v.toString();
  if (typeof v === "boolean") return v ? "true" : "false";
  return quoteString(v);
}

function encodeArray(v: string[]): string {
  return `[${v.map(encodeValue).join(", ")}]`;
}

const LEAF_OP: Partial<Record<ConditionType, string>> = {
  EQUALS: "=",
  MATCHES: "~",
  CONTAINS: "CONTAINS",
  LT: "<",
  LE: "<=",
  GT: ">",
  GE: ">=",
};

const LOGICAL_OPS = new Set<ConditionType>(["AND", "OR", "NOT"]);

function encodeChild(child: Condition, parent: ConditionType): string {
  const out = encodeText(child);
  // Always wrap logical-op children inside another logical op for unambiguous output.
  // Leaf nodes (EXISTS, EQUALS, etc.) are already atomic and never need parens.
  if (LOGICAL_OPS.has(child.type) && LOGICAL_OPS.has(parent)) return `(${out})`;
  return out;
}

export function encodeText(c: Condition): string {
  switch (c.type) {
    case "ALWAYS_TRUE":
      return "";
    case "SEARCH":
      return quoteString(c.value);
    case "EXISTS":
      return `${encodeIdent(c.field)}?`;
    case "IN":
      return `${encodeIdent(c.field)} IN ${encodeArray(c.value)}`;
    case "EQUALS":
    case "MATCHES":
    case "CONTAINS":
    case "LT":
    case "LE":
    case "GT":
    case "GE": {
      const op = LEAF_OP[c.type]!;
      return `${encodeIdent(c.field)} ${op} ${encodeValue(c.value)}`;
    }
    case "NOT":
      return `NOT ${encodeChild(c.arg, "NOT")}`;
    case "AND":
      return c.args.map((a) => encodeChild(a, "AND")).join(" AND ");
    case "OR":
      return c.args.map((a) => encodeChild(a, "OR")).join(" OR ");
  }
}

// ---------------- lexer ----------------

type TokKind =
  | "IDENT"
  | "STRING"
  | "NUMBER"
  | "LPAREN"
  | "RPAREN"
  | "LBRACK"
  | "RBRACK"
  | "COMMA"
  | "EQ"
  | "LT"
  | "LE"
  | "GT"
  | "GE"
  | "TILDE"
  | "QUESTION"
  | "AND"
  | "OR"
  | "NOT"
  | "MATCHES_KW"
  | "CONTAINS_KW"
  | "IN_KW"
  | "EXISTS_KW"
  | "EOF";

type Tok = { kind: TokKind; text: string; pos: number; num?: number };

function isIdentStart(ch: string) {
  return /[A-Za-z_]/.test(ch);
}
function isIdentCont(ch: string) {
  return /[A-Za-z0-9_.-]/.test(ch);
}

const KEYWORDS: Record<string, TokKind> = {
  AND: "AND",
  OR: "OR",
  NOT: "NOT",
  MATCHES: "MATCHES_KW",
  CONTAINS: "CONTAINS_KW",
  IN: "IN_KW",
  EXISTS: "EXISTS_KW",
};

function lex(src: string): Tok[] | TextParseError {
  const out: Tok[] = [];
  let i = 0;
  while (i < src.length) {
    const ch = src[i]!;
    if (/\s/.test(ch)) {
      i++;
      continue;
    }
    if (ch === "(") {
      out.push({ kind: "LPAREN", text: "(", pos: i });
      i++;
      continue;
    }
    if (ch === ")") {
      out.push({ kind: "RPAREN", text: ")", pos: i });
      i++;
      continue;
    }
    if (ch === "[") {
      out.push({ kind: "LBRACK", text: "[", pos: i });
      i++;
      continue;
    }
    if (ch === "]") {
      out.push({ kind: "RBRACK", text: "]", pos: i });
      i++;
      continue;
    }
    if (ch === ",") {
      out.push({ kind: "COMMA", text: ",", pos: i });
      i++;
      continue;
    }
    if (ch === "=") {
      out.push({ kind: "EQ", text: "=", pos: i });
      i++;
      continue;
    }
    if (ch === "~") {
      out.push({ kind: "TILDE", text: "~", pos: i });
      i++;
      continue;
    }
    if (ch === "?") {
      out.push({ kind: "QUESTION", text: "?", pos: i });
      i++;
      continue;
    }
    if (ch === "&") {
      out.push({ kind: "AND", text: "&", pos: i });
      i++;
      continue;
    }
    if (ch === "|") {
      out.push({ kind: "OR", text: "|", pos: i });
      i++;
      continue;
    }
    if (ch === "!") {
      out.push({ kind: "NOT", text: "!", pos: i });
      i++;
      continue;
    }
    if (ch === "<") {
      if (src[i + 1] === "=") {
        out.push({ kind: "LE", text: "<=", pos: i });
        i += 2;
        continue;
      }
      out.push({ kind: "LT", text: "<", pos: i });
      i++;
      continue;
    }
    if (ch === ">") {
      if (src[i + 1] === "=") {
        out.push({ kind: "GE", text: ">=", pos: i });
        i += 2;
        continue;
      }
      out.push({ kind: "GT", text: ">", pos: i });
      i++;
      continue;
    }
    if (ch === '"' || ch === "'") {
      const quote = ch;
      const startPos = i;
      i++;
      let buf = "";
      while (i < src.length && src[i] !== quote) {
        if (src[i] === "\\" && i + 1 < src.length) {
          const next = src[i + 1]!;
          if (next === "n") buf += "\n";
          else if (next === "t") buf += "\t";
          else buf += next;
          i += 2;
          continue;
        }
        buf += src[i]!;
        i++;
      }
      if (i >= src.length) return { message: "unterminated string", pos: startPos };
      i++; // closing quote
      out.push({ kind: "STRING", text: buf, pos: startPos });
      continue;
    }
    if (/[0-9]/.test(ch) || (ch === "-" && /[0-9]/.test(src[i + 1] ?? ""))) {
      const startPos = i;
      let j = i;
      if (src[j] === "-") j++;
      while (j < src.length && /[0-9]/.test(src[j]!)) j++;
      if (src[j] === "." && /[0-9]/.test(src[j + 1] ?? "")) {
        j++;
        while (j < src.length && /[0-9]/.test(src[j]!)) j++;
      }
      const raw = src.slice(i, j);
      out.push({ kind: "NUMBER", text: raw, pos: startPos, num: Number(raw) });
      i = j;
      continue;
    }
    if (isIdentStart(ch)) {
      const startPos = i;
      let j = i + 1;
      while (j < src.length && isIdentCont(src[j]!)) j++;
      const word = src.slice(i, j);
      const kw = KEYWORDS[word.toUpperCase()];
      if (kw) out.push({ kind: kw, text: word, pos: startPos });
      else out.push({ kind: "IDENT", text: word, pos: startPos });
      i = j;
      continue;
    }
    return { message: `unexpected character ${JSON.stringify(ch)}`, pos: i };
  }
  out.push({ kind: "EOF", text: "", pos: src.length });
  return out;
}

// ---------------- parser ----------------

class Parser {
  pos = 0;
  constructor(public toks: Tok[]) {}
  peek(): Tok {
    return this.toks[this.pos]!;
  }
  eat(): Tok {
    const t = this.peek();
    if (t.kind !== "EOF") this.pos++;
    return t;
  }
  err(t: Tok, msg: string): TextParseError {
    return { message: msg, pos: t.pos };
  }
}

function parseValue(
  p: Parser,
): { value: string | number | string[]; isArray: boolean } | TextParseError {
  const t = p.peek();
  if (t.kind === "STRING") {
    p.eat();
    return { value: t.text, isArray: false };
  }
  if (t.kind === "NUMBER") {
    p.eat();
    return { value: t.num!, isArray: false };
  }
  if (t.kind === "IDENT") {
    p.eat();
    return { value: t.text, isArray: false };
  }
  if (t.kind === "LBRACK") {
    p.eat();
    const items: string[] = [];
    if (p.peek().kind !== "RBRACK") {
      for (;;) {
        const v = parseValue(p);
        if ("message" in v) return v;
        if (v.isArray) return p.err(t, "nested arrays not supported");
        items.push(String(v.value));
        if (p.peek().kind === "COMMA") {
          p.eat();
          continue;
        }
        break;
      }
    }
    if (p.peek().kind !== "RBRACK") return p.err(p.peek(), "expected ]");
    p.eat();
    return { value: items, isArray: true };
  }
  return p.err(t, "expected value");
}

function parseTerm(p: Parser): Condition | TextParseError {
  const t = p.peek();
  if (t.kind === "LPAREN") {
    p.eat();
    const c = parseExpr(p);
    if ("message" in c) return c;
    if (p.peek().kind !== "RPAREN") return p.err(p.peek(), "expected )");
    p.eat();
    return c;
  }
  // <ident-or-string> followed by op, or trailing ? / EXISTS, or bare → SEARCH
  if (t.kind === "IDENT" || t.kind === "STRING") {
    const fieldTok = p.eat();
    const field = fieldTok.text;
    const next = p.peek();
    if (next.kind === "QUESTION") {
      p.eat();
      return { type: "EXISTS", field };
    }
    if (next.kind === "EXISTS_KW") {
      p.eat();
      return { type: "EXISTS", field };
    }
    const opMap: Partial<Record<TokKind, Condition["type"]>> = {
      EQ: "EQUALS",
      TILDE: "MATCHES",
      MATCHES_KW: "MATCHES",
      CONTAINS_KW: "CONTAINS",
      IN_KW: "IN",
      LT: "LT",
      LE: "LE",
      GT: "GT",
      GE: "GE",
    };
    const op = opMap[next.kind];
    if (op) {
      p.eat();
      const v = parseValue(p);
      if ("message" in v) return v;
      if (op === "IN") {
        if (!v.isArray) return p.err(next, "IN requires an array on the right");
        return { type: "IN", field, value: v.value as string[] };
      }
      if (op === "LT" || op === "LE" || op === "GT" || op === "GE") {
        if (typeof v.value !== "number") return p.err(next, "comparison requires number");
        return { type: op, field, value: v.value };
      }
      if (v.isArray) return p.err(next, "operator does not accept array");
      return { type: op, field, value: String(v.value) } as Condition;
    }
    // No operator → it was a bare value → SEARCH
    return { type: "SEARCH", field: "", value: field };
  }
  if (t.kind === "NUMBER") {
    p.eat();
    return { type: "SEARCH", field: "", value: t.text };
  }
  return p.err(t, "unexpected token");
}

function parseNot(p: Parser): Condition | TextParseError {
  if (p.peek().kind === "NOT") {
    p.eat();
    const inner = parseNot(p);
    if ("message" in inner) return inner;
    return { type: "NOT", arg: inner };
  }
  return parseTerm(p);
}

function parseAnd(p: Parser): Condition | TextParseError {
  const first = parseNot(p);
  if ("message" in first) return first;
  const args: Condition[] = [first];
  for (;;) {
    const t = p.peek();
    if (t.kind === "AND") {
      p.eat();
    } else if (
      t.kind === "IDENT" ||
      t.kind === "STRING" ||
      t.kind === "NUMBER" ||
      t.kind === "LPAREN" ||
      t.kind === "NOT"
    ) {
      // implicit AND — do not consume
    } else {
      break;
    }
    const next = parseNot(p);
    if ("message" in next) return next;
    args.push(next);
  }
  return args.length === 1 ? args[0]! : { type: "AND", args };
}

function parseOr(p: Parser): Condition | TextParseError {
  const first = parseAnd(p);
  if ("message" in first) return first;
  const args: Condition[] = [first];
  while (p.peek().kind === "OR") {
    p.eat();
    const next = parseAnd(p);
    if ("message" in next) return next;
    args.push(next);
  }
  return args.length === 1 ? args[0]! : { type: "OR", args };
}

function parseExpr(p: Parser): Condition | TextParseError {
  return parseOr(p);
}

export function parseText(src: string): TextParseResult {
  const trimmed = src.trim();
  if (trimmed === "") return { ok: true, value: { type: "ALWAYS_TRUE" } };
  const toks = lex(src);
  if (!Array.isArray(toks)) return { ok: false, error: toks };
  const p = new Parser(toks);
  const c = parseExpr(p);
  if ("message" in c) return { ok: false, error: c };
  if (p.peek().kind !== "EOF") {
    return { ok: false, error: p.err(p.peek(), "unexpected trailing input") };
  }
  return { ok: true, value: c };
}
