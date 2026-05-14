import type { Condition } from "./types";
export type { Condition } from "./types";

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
