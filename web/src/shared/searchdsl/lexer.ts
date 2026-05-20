// Lexer for the Snooze search DSL.
//
// This is a faithful port of internal/condition/lexer.go used only for live
// syntax highlighting in the SearchBar. The Go parser remains the source of
// truth for semantic validation — the SearchBar always round-trips the
// query string through POST /api/v1/condition/parse for the AST and any
// error position.
//
// Why a separate TS lexer at all? Because routing every keystroke through
// the server for a coloured underline would be wasteful and would break the
// hover-help features that need to know "what's at byte 17". Highlighting
// is read-only; staying in lockstep with Go is acceptable as long as we
// don't drift on operator names — which the test corpus mirrors.

export type TokenKind =
  | "eof"
  | "ident"
  | "string"
  | "number"
  | "bool"
  | "lbrack"
  | "rbrack"
  | "lbrace"
  | "rbrace"
  | "lparen"
  | "rparen"
  | "comma"
  | "colon"
  | "eq"
  | "neq"
  | "lt"
  | "lte"
  | "gt"
  | "gte"
  | "matches"
  | "exists_sym"
  | "contains"
  | "in"
  | "and"
  | "or"
  | "not"
  | "exists_kw"
  | "error";

export type Token = {
  kind: TokenKind;
  /** Byte offset of the token start in the source. */
  pos: number;
  /** Byte length of the token in the source (including any quotes). */
  len: number;
  /** Original source text of the token (raw slice). */
  text: string;
  /** Decoded body, for strings. */
  value?: string;
};

/** Token kinds that map to a "keyword" highlight class. */
export const KEYWORD_KINDS = new Set<TokenKind>([
  "and",
  "or",
  "not",
  "matches",
  "exists_kw",
  "contains",
  "in",
  "bool",
]);

/** Token kinds that map to an "operator" highlight class. */
export const OPERATOR_KINDS = new Set<TokenKind>([
  "eq",
  "neq",
  "lt",
  "lte",
  "gt",
  "gte",
  "exists_sym",
]);

/** Token kinds that map to a "punct" highlight class. */
export const PUNCT_KINDS = new Set<TokenKind>([
  "lparen",
  "rparen",
  "lbrack",
  "rbrack",
  "lbrace",
  "rbrace",
  "comma",
  "colon",
]);

/**
 * Tokenize the given source. Whitespace is consumed silently, never produced.
 * The terminating EOF token is appended so the consumer can use `kind ===
 * "eof"` as a loop guard.
 *
 * The lexer is permissive: unterminated strings and invalid characters
 * produce a single "error" token covering the offending range, but lexing
 * does not throw. That keeps the highlighter responsive even on
 * half-typed input.
 */
export function tokenize(src: string): Token[] {
  const out: Token[] = [];
  let i = 0;
  const n = src.length;

  const isSpace = (c: string) => /\s/.test(c);
  const isDigit = (c: string) => c >= "0" && c <= "9";
  const isIdentStart = (c: string) =>
    c === "_" || c === "." || c === "-" || /[a-zA-Z]/.test(c) || isDigit(c);
  const isIdentPart = (c: string) =>
    c === "_" || c === "." || c === "-" || /[a-zA-Z]/.test(c) || isDigit(c);

  while (i < n) {
    while (i < n && isSpace(src[i] as string)) i++;
    if (i >= n) break;
    const start = i;
    const c = src[i] as string;

    // Single-character punctuation.
    switch (c) {
      case "(":
        i++;
        out.push({ kind: "lparen", pos: start, len: 1, text: c });
        continue;
      case ")":
        i++;
        out.push({ kind: "rparen", pos: start, len: 1, text: c });
        continue;
      case "[":
        i++;
        out.push({ kind: "lbrack", pos: start, len: 1, text: c });
        continue;
      case "]":
        i++;
        out.push({ kind: "rbrack", pos: start, len: 1, text: c });
        continue;
      case "{":
        i++;
        out.push({ kind: "lbrace", pos: start, len: 1, text: c });
        continue;
      case "}":
        i++;
        out.push({ kind: "rbrace", pos: start, len: 1, text: c });
        continue;
      case ",":
        i++;
        out.push({ kind: "comma", pos: start, len: 1, text: c });
        continue;
      case ":":
        i++;
        out.push({ kind: "colon", pos: start, len: 1, text: c });
        continue;
      case "&":
        i++;
        out.push({ kind: "and", pos: start, len: 1, text: c });
        continue;
      case "|":
        i++;
        out.push({ kind: "or", pos: start, len: 1, text: c });
        continue;
      case "~":
        i++;
        out.push({ kind: "matches", pos: start, len: 1, text: c });
        continue;
      case "?":
        i++;
        out.push({ kind: "exists_sym", pos: start, len: 1, text: c });
        continue;
      case "=":
        i++;
        out.push({ kind: "eq", pos: start, len: 1, text: c });
        continue;
    }

    // Two-character operators.
    if (c === "!") {
      if (src[i + 1] === "=") {
        i += 2;
        out.push({ kind: "neq", pos: start, len: 2, text: "!=" });
        continue;
      }
      i++;
      out.push({ kind: "not", pos: start, len: 1, text: "!" });
      continue;
    }
    if (c === "<") {
      if (src[i + 1] === "=") {
        i += 2;
        out.push({ kind: "lte", pos: start, len: 2, text: "<=" });
        continue;
      }
      i++;
      out.push({ kind: "lt", pos: start, len: 1, text: "<" });
      continue;
    }
    if (c === ">") {
      if (src[i + 1] === "=") {
        i += 2;
        out.push({ kind: "gte", pos: start, len: 2, text: ">=" });
        continue;
      }
      i++;
      out.push({ kind: "gt", pos: start, len: 1, text: ">" });
      continue;
    }

    // String literals.
    if (c === '"' || c === "'") {
      const quote = c;
      let j = i + 1;
      let body = "";
      let closed = false;
      while (j < n) {
        const cj = src[j] as string;
        if (cj === "\\") {
          if (j + 1 >= n) {
            j = n;
            break;
          }
          const esc = src[j + 1] as string;
          switch (esc) {
            case "n":
              body += "\n";
              break;
            case "t":
              body += "\t";
              break;
            case "r":
              body += "\r";
              break;
            case "\\":
              body += "\\";
              break;
            case '"':
              body += '"';
              break;
            case "'":
              body += "'";
              break;
            case "/":
              body += "/";
              break;
            case "0":
              body += "\0";
              break;
            default:
              body += esc;
          }
          j += 2;
          continue;
        }
        if (cj === quote) {
          closed = true;
          j++;
          break;
        }
        body += cj;
        j++;
      }
      if (!closed) {
        out.push({ kind: "error", pos: start, len: j - start, text: src.slice(start, j) });
      } else {
        out.push({
          kind: "string",
          pos: start,
          len: j - start,
          text: src.slice(start, j),
          value: body,
        });
      }
      i = j;
      continue;
    }

    // Numbers (with optional sign).
    if (c === "-" || c === "+" || c === "." || isDigit(c)) {
      const tok = tryReadNumber(src, i);
      if (tok) {
        out.push(tok);
        i = tok.pos + tok.len;
        continue;
      }
    }

    // Identifiers (and keywords).
    if (isIdentStart(c)) {
      let j = i + 1;
      while (j < n && isIdentPart(src[j] as string)) j++;
      const text = src.slice(i, j);
      const upper = text.toUpperCase();
      let kind: TokenKind = "ident";
      switch (upper) {
        case "AND":
          kind = "and";
          break;
        case "OR":
          kind = "or";
          break;
        case "NOT":
          kind = "not";
          break;
        case "MATCHES":
          kind = "matches";
          break;
        case "EXISTS":
          kind = "exists_kw";
          break;
        case "CONTAINS":
          kind = "contains";
          break;
        case "IN":
          kind = "in";
          break;
        case "TRUE":
        case "FALSE":
          kind = "bool";
          break;
      }
      out.push({ kind, pos: i, len: j - i, text });
      i = j;
      continue;
    }

    // Anything else: single-char error token so we keep advancing.
    out.push({ kind: "error", pos: start, len: 1, text: c });
    i++;
  }

  out.push({ kind: "eof", pos: n, len: 0, text: "" });
  return out;
}

/**
 * tryReadNumber consumes a numeric literal at `start`, returning the token if
 * the bytes form a valid number, or null if they do not (in which case the
 * caller should fall through to the identifier branch — the Go lexer
 * mirrors this rollback behaviour for cases like `-foo`).
 */
function tryReadNumber(src: string, start: number): Token | null {
  let i = start;
  const n = src.length;
  if (i < n && (src[i] === "+" || src[i] === "-")) i++;
  let hadDigit = false;
  while (i < n && src[i]! >= "0" && src[i]! <= "9") {
    hadDigit = true;
    i++;
  }
  if (i < n && src[i] === ".") {
    // Only treat '.' as fractional if followed by a digit.
    if (i + 1 < n && src[i + 1]! >= "0" && src[i + 1]! <= "9") {
      i++;
      while (i < n && src[i]! >= "0" && src[i]! <= "9") {
        hadDigit = true;
        i++;
      }
    }
  }
  if (i < n && (src[i] === "e" || src[i] === "E")) {
    const expStart = i;
    i++;
    if (i < n && (src[i] === "+" || src[i] === "-")) i++;
    let expHadDigit = false;
    while (i < n && src[i]! >= "0" && src[i]! <= "9") {
      expHadDigit = true;
      i++;
    }
    if (!expHadDigit) i = expStart;
  }
  if (!hadDigit) return null;
  // If the next char would extend into an identifier, the input wasn't a
  // number — fall through. Mirrors the Go lexer's rollback.
  if (i < n) {
    const c = src[i] as string;
    const isIdentExt =
      c === "_" || /[a-zA-Z]/.test(c) || (c !== "-" && c !== "." && /[0-9]/.test(c));
    if (isIdentExt) return null;
  }
  return { kind: "number", pos: start, len: i - start, text: src.slice(start, i) };
}
