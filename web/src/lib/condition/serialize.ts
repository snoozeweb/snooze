export type Condition =
  | { type: "ALWAYS_TRUE" }
  | { type: "EQUALS" | "CONTAINS" | "MATCHES" | "SEARCH"; field: string; value: string }
  | { type: "IN"; field: string; value: string[] }
  | { type: "LT" | "GT" | "LE" | "GE"; field: string; value: number }
  | { type: "EXISTS"; field: string }
  | { type: "NOT"; arg: Condition }
  | { type: "AND" | "OR"; args: Condition[] };

export function encodeConditionQ(cond: Condition): string {
  return base64UrlEncode(JSON.stringify(cond));
}

function base64UrlEncode(input: string): string {
  let bytes: string;
  if (typeof TextEncoder !== "undefined") {
    const enc = new TextEncoder().encode(input);
    bytes = String.fromCharCode(...enc);
  } else {
    bytes = unescape(encodeURIComponent(input));
  }
  return btoa(bytes).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}
