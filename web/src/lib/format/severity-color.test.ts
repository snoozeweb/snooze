// web/src/lib/format/severity-color.test.ts
import { describe, it, expect, beforeEach } from "vitest";
import { severityColor, severityToken } from "./severity-color";

beforeEach(() => {
  const root = document.documentElement;
  root.style.setProperty("--severity-critical", "#f04949");
  root.style.setProperty("--severity-error", "#ef7e3a");
  root.style.setProperty("--severity-warning", "#d4a017");
  root.style.setProperty("--severity-info", "#4f8cff");
  root.style.setProperty("--severity-ok", "#3fb950");
  root.style.setProperty("--text-muted", "#6b7785");
});

describe("severityColor", () => {
  it("uses the canonical token for the canonical label", () => {
    expect(severityColor("critical").toLowerCase()).toBe("#f04949");
    expect(severityColor("error").toLowerCase()).toBe("#ef7e3a");
    expect(severityColor("warning").toLowerCase()).toBe("#d4a017");
    expect(severityColor("ok").toLowerCase()).toBe("#3fb950");
  });
  it("emergency is a DARKER red than critical (more serious)", () => {
    const crit = severityColor("critical");
    const emerg = severityColor("emergency");
    expect(emerg).not.toBe(crit);
    expect(luminance(emerg)).toBeLessThan(luminance(crit));
  });
  it("alert sits between critical and emergency", () => {
    expect(luminance(severityColor("alert"))).toBeLessThan(luminance(severityColor("critical")));
    expect(luminance(severityColor("alert"))).toBeGreaterThan(
      luminance(severityColor("emergency")),
    );
  });
  it("error stays orange, never folded into the reds (option X)", () => {
    expect(severityColor("error").toLowerCase()).toBe("#ef7e3a");
  });
  it("unknown labels fall back to muted", () => {
    expect(severityColor("banana").toLowerCase()).toBe("#6b7785");
  });
});

describe("severityToken", () => {
  it("maps canonical labels to their raw var(--severity-*) token", () => {
    expect(severityToken("critical")).toBe("var(--severity-critical)");
    expect(severityToken("error")).toBe("var(--severity-error)");
    expect(severityToken("warning")).toBe("var(--severity-warning)");
    expect(severityToken("info")).toBe("var(--severity-info)");
    expect(severityToken("ok")).toBe("var(--severity-ok)");
  });
  it("collapses syslog aliases to the same token as their variant (no tint)", () => {
    // emergency/alert all sit in the `critical` variant — unlike severityColor
    // they share one token (the tint is intentionally dropped for accents).
    expect(severityToken("emergency")).toBe("var(--severity-critical)");
    expect(severityToken("alert")).toBe("var(--severity-critical)");
    expect(severityToken("crit")).toBe("var(--severity-critical)");
  });
  it("returns undefined for unknown/blank labels (no accent strip)", () => {
    expect(severityToken("banana")).toBeUndefined();
    expect(severityToken("")).toBeUndefined();
  });
});

function luminance(hex: string): number {
  const h = hex.replace("#", "");
  const r = parseInt(h.slice(0, 2), 16),
    g = parseInt(h.slice(2, 4), 16),
    b = parseInt(h.slice(4, 6), 16);
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}
