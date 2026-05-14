import type { Condition } from "./types";

export function decodeConditionQ(q: string): Condition | null {
  if (!q) return null;
  try {
    const json = base64UrlDecode(q);
    return JSON.parse(json) as Condition;
  } catch {
    return null;
  }
}

function base64UrlDecode(s: string): string {
  const pad = (4 - (s.length % 4)) % 4;
  const padded = s + "=".repeat(pad);
  const std = padded.replace(/-/g, "+").replace(/_/g, "/");
  const binary = atob(std);
  if (typeof TextDecoder !== "undefined") {
    const bytes = Uint8Array.from(binary, (c) => c.charCodeAt(0));
    return new TextDecoder().decode(bytes);
  }
  return decodeURIComponent(escape(binary));
}
