// nameQuery encodes a value into a search-DSL equality on `name`, e.g.
// `name = "web-01"` — the ?search= a deep link to a named object (a snooze, an
// action, a rule) lands on. Backslash and double-quote are escaped to match
// the lexer's string rules (shared/searchdsl/lexer.ts) so names containing
// spaces or quotes still round-trip through the target page's SearchBar.
export function nameQuery(value: string): string {
  const escaped = value.replace(/\\/g, "\\\\").replace(/"/g, '\\"');
  return `name = "${escaped}"`;
}
