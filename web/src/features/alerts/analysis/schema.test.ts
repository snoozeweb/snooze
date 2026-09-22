import { describe, expect, it } from "vitest";
import type { FieldErrors } from "react-hook-form";
import type { components } from "@/lib/api/types.gen";
import {
  ANALYSIS_LIMITS,
  ANALYSIS_SOURCE,
  analysisResolver,
  analysisToForm,
  emptyAnalysisForm,
  formToRequest,
  runeLength,
  toFieldErrors,
  validateAnalysisForm,
  type AnalysisForm,
  type AnalysisFormContext,
} from "./schema";

/** The stored subtree, as the editor is seeded from it. */
type StoredAgentic = components["schemas"]["Agentic"];

/** A form the server would accept, as the starting point for each case. */
function validForm(): AnalysisForm {
  return {
    root_cause: {
      summary: "The /var filesystem filled up with rotated journals.",
      scope: "srv-victoria1:/var",
      evidence: ["df -h /var: 100% used"],
      confidence: "high",
    },
    remediation_plan: {
      steps: [
        { action: "Vacuum the journal", command: "journalctl --vacuum-size=200M", risk: "low" },
      ],
      rollback: [],
      automatable: true,
    },
  };
}

/**
 * `n` code points of a 4-byte emoji — one rune each, but TWO UTF-16 units
 * each, which is the whole point: `String.length` would double-count them.
 */
function emoji(n: number): string {
  return "\u{1F6E0}".repeat(n);
}

describe("runeLength", () => {
  it("counts code points, not UTF-16 units — the server counts runes", () => {
    const s = emoji(500);
    expect(runeLength(s)).toBe(500);
    // The naive count would have rejected this string as twice the limit.
    expect(s.length).toBe(1000);
  });
});

describe("validateAnalysisForm", () => {
  it("accepts a well-formed analysis", () => {
    expect(validateAnalysisForm(validForm())).toEqual({});
  });

  it("requires a summary, and treats whitespace as absent", () => {
    const form = validForm();
    form.root_cause.summary = "   \n ";
    expect(validateAnalysisForm(form)).toEqual({ "root_cause.summary": "is required" });
  });

  it("caps the summary at 500 characters, counted as runes", () => {
    const form = validForm();
    form.root_cause.summary = emoji(ANALYSIS_LIMITS.summary);
    expect(validateAnalysisForm(form)).toEqual({});

    form.root_cause.summary = emoji(ANALYSIS_LIMITS.summary + 1);
    expect(validateAnalysisForm(form)).toEqual({
      "root_cause.summary": "must be at most 500 characters",
    });
  });

  it("caps the scope at 200 characters but does not require one", () => {
    const form = validForm();
    form.root_cause.scope = "";
    expect(validateAnalysisForm(form)).toEqual({});

    form.root_cause.scope = "s".repeat(ANALYSIS_LIMITS.scope);
    expect(validateAnalysisForm(form)).toEqual({});

    form.root_cause.scope = "s".repeat(ANALYSIS_LIMITS.scope + 1);
    expect(validateAnalysisForm(form)).toEqual({
      "root_cause.scope": "must be at most 200 characters",
    });
  });

  it("requires a confidence and closes the enum", () => {
    const form = validForm();
    form.root_cause.confidence = "";
    expect(validateAnalysisForm(form)).toEqual({
      "root_cause.confidence": "is required (high|medium|low)",
    });

    form.root_cause.confidence = "certain" as never;
    expect(validateAnalysisForm(form)).toEqual({
      "root_cause.confidence": "must be one of high|medium|low",
    });
  });

  it("indexes evidence failures at the server's exact path", () => {
    const form = validForm();
    form.root_cause.evidence = ["ok", "also ok", "   ", "x".repeat(ANALYSIS_LIMITS.evidence + 1)];
    expect(validateAnalysisForm(form)).toEqual({
      "root_cause.evidence[2]": "must not be empty",
      "root_cause.evidence[3]": "must be at most 500 characters",
    });
  });

  it("caps evidence at 10 items and still reports the bad ones", () => {
    const form = validForm();
    form.root_cause.evidence = Array.from({ length: 11 }, (_, i) => (i === 4 ? "" : `line ${i}`));
    expect(validateAnalysisForm(form)).toEqual({
      "root_cause.evidence": "must hold at most 10 items",
      "root_cause.evidence[4]": "must not be empty",
    });
  });

  it("requires at least one step", () => {
    const form = validForm();
    form.remediation_plan.steps = [];
    expect(validateAnalysisForm(form)).toEqual({
      "remediation_plan.steps": "must hold at least one step",
    });
  });

  it("caps each list at 20 steps and stops walking members, like the server", () => {
    const form = validForm();
    form.remediation_plan.steps = Array.from({ length: 21 }, () => ({
      action: "",
      command: "",
      risk: "" as const,
    }));
    expect(validateAnalysisForm(form)).toEqual({
      "remediation_plan.steps": "must hold at most 20 steps",
    });
  });

  it("allows an empty rollback but validates one that is present", () => {
    const form = validForm();
    expect(validateAnalysisForm(form)).toEqual({});

    form.remediation_plan.rollback = [
      { action: "Restore the snapshot", command: "", risk: "high" },
      { action: "", command: "", risk: "" },
    ];
    expect(validateAnalysisForm(form)).toEqual({
      "remediation_plan.rollback[1].action": "is required",
      "remediation_plan.rollback[1].risk": "is required (low|medium|high)",
    });
  });

  it("requires a step action, caps it at 500, and caps the command at 1000", () => {
    const form = validForm();
    form.remediation_plan.steps = [
      { action: " ", command: "", risk: "low" },
      { action: emoji(ANALYSIS_LIMITS.action + 1), command: "", risk: "low" },
      { action: "ok", command: emoji(ANALYSIS_LIMITS.command), risk: "low" },
      { action: "ok", command: "c".repeat(ANALYSIS_LIMITS.command + 1), risk: "low" },
    ];
    expect(validateAnalysisForm(form)).toEqual({
      "remediation_plan.steps[0].action": "is required",
      "remediation_plan.steps[1].action": "must be at most 500 characters",
      "remediation_plan.steps[3].command": "must be at most 1000 characters",
    });
  });

  it("closes the risk enum per step", () => {
    const form = validForm();
    form.remediation_plan.steps = [
      { action: "ok", command: "", risk: "" },
      { action: "ok", command: "", risk: "extreme" as never },
    ];
    expect(validateAnalysisForm(form)).toEqual({
      "remediation_plan.steps[0].risk": "is required (low|medium|high)",
      "remediation_plan.steps[1].risk": "must be one of low|medium|high",
    });
  });

  it("rejects a NUL anywhere — Postgres jsonb cannot store one", () => {
    const form = validForm();
    form.root_cause.summary = `cause\u0000`;
    form.root_cause.scope = `scope\u0000`;
    form.root_cause.evidence = [`ev\u0000`];
    form.remediation_plan.steps = [{ action: `do\u0000`, command: `run\u0000`, risk: "low" }];
    expect(validateAnalysisForm(form)).toEqual({
      "root_cause.summary": "must not contain the NUL character",
      "root_cause.scope": "must not contain the NUL character",
      "root_cause.evidence[0]": "must not contain the NUL character",
      "remediation_plan.steps[0].action": "must not contain the NUL character",
      "remediation_plan.steps[0].command": "must not contain the NUL character",
    });
  });

  it("caps the source tag at 64 characters when one is supplied", () => {
    const form = validForm();
    expect(validateAnalysisForm(form, ANALYSIS_SOURCE)).toEqual({});
    expect(validateAnalysisForm(form, "s".repeat(ANALYSIS_LIMITS.source))).toEqual({});
    expect(validateAnalysisForm(form, "s".repeat(ANALYSIS_LIMITS.source + 1))).toEqual({
      source: "must be at most 64 characters",
    });
    expect(validateAnalysisForm(form, "tool\u0000")).toEqual({
      source: "must not contain the NUL character",
    });
  });

  it("reports every failure at once, not just the first", () => {
    const form = emptyAnalysisForm();
    expect(Object.keys(validateAnalysisForm(form)).sort()).toEqual([
      "remediation_plan.steps[0].action",
      "remediation_plan.steps[0].risk",
      "root_cause.confidence",
      "root_cause.summary",
    ]);
  });
});

describe("analysisResolver", () => {
  /** Drives the resolver the way react-hook-form does. */
  async function resolve(form: AnalysisForm, context?: AnalysisFormContext) {
    const result = await analysisResolver(form, context, {
      fields: {},
      shouldUseNativeValidation: false,
    });
    return { values: result.values, errors: result.errors as FieldErrors<AnalysisForm> };
  }

  it("passes the values through when the form is valid", async () => {
    const form = validForm();
    const result = await resolve(form);
    expect(result.errors).toEqual({});
    expect(result.values).toEqual(form);
  });

  it("nests the flat server paths into the tree react-hook-form renders from", async () => {
    const { values, errors } = await resolve(emptyAnalysisForm());
    expect(values).toEqual({});
    expect(errors.root_cause?.summary?.message).toBe("is required");
    expect(errors.root_cause?.confidence?.message).toBe("is required (high|medium|low)");
    expect(errors.remediation_plan?.steps?.[0]?.action?.message).toBe("is required");
    expect(errors.remediation_plan?.steps?.[0]?.risk?.message).toBe(
      "is required (low|medium|high)",
    );
  });

  it("checks the source carried on the resolver context", async () => {
    const { errors } = await resolve(validForm(), { source: "s".repeat(65) });
    expect(errors).toEqual({
      source: { type: "validate", message: "must be at most 64 characters" },
    });
  });
});

describe("toFieldErrors", () => {
  it("lifts a server 422 details map onto the same fields the resolver uses", () => {
    // This is the merge that makes local and remote validation one thing: the
    // server's bracket notation has to land where the resolver's does.
    const tree = toFieldErrors({
      "root_cause.confidence": "must be one of high|medium|low",
      "remediation_plan.steps[0].risk": "must be one of low|medium|high",
      "remediation_plan.rollback[1].action": "is required",
    });
    expect(tree.root_cause?.confidence?.message).toBe("must be one of high|medium|low");
    expect(tree.remediation_plan?.steps?.[0]?.risk?.message).toBe("must be one of low|medium|high");
    expect(tree.remediation_plan?.rollback?.[1]?.action?.message).toBe("is required");
  });
});

describe("emptyAnalysisForm", () => {
  it("opens with one blank step and no pre-picked confidence", () => {
    const form = emptyAnalysisForm();
    expect(form.remediation_plan.steps).toHaveLength(1);
    expect(form.remediation_plan.rollback).toEqual([]);
    expect(form.remediation_plan.automatable).toBe(false);
    // "" and not "low": the author must state a confidence rather than inherit
    // a guess that reads as a real judgement to every downstream consumer.
    expect(form.root_cause.confidence).toBe("");
    expect(form.root_cause.evidence).toEqual([]);
  });

  it("is not shared state between calls", () => {
    const a = emptyAnalysisForm();
    a.remediation_plan.steps[0]!.action = "mutated";
    expect(emptyAnalysisForm().remediation_plan.steps[0]!.action).toBe("");
  });
});

describe("analysisToForm", () => {
  it("widens every optional server field to its empty form value", () => {
    expect(analysisToForm({ root_cause: { summary: "boom", confidence: "low" } })).toEqual({
      root_cause: { summary: "boom", scope: "", evidence: [], confidence: "low" },
      remediation_plan: {
        steps: [{ action: "", command: "", risk: "" }],
        rollback: [],
        automatable: false,
      },
    });
  });

  it("round-trips a full analysis", () => {
    const form = validForm();
    expect(analysisToForm(formToRequest(form))).toEqual(form);
  });

  it("drops an out-of-range enum rather than keeping a value no select can show", () => {
    const form = analysisToForm({
      root_cause: { summary: "boom", confidence: "certain" as never },
      remediation_plan: { steps: [{ action: "do", risk: "extreme" as never }] },
    });
    expect(form.root_cause.confidence).toBe("");
    expect(form.remediation_plan.steps[0]!.risk).toBe("");
  });

  it("returns the empty form for a missing analysis", () => {
    expect(analysisToForm(null)).toEqual(emptyAnalysisForm());
    expect(analysisToForm(undefined)).toEqual(emptyAnalysisForm());
  });
});

describe("formToRequest", () => {
  it("omits every empty optional instead of storing it blank", () => {
    const form = emptyAnalysisForm();
    form.root_cause.summary = "boom";
    form.root_cause.confidence = "low";
    form.remediation_plan.steps = [{ action: "restart", command: "  ", risk: "low" }];
    expect(formToRequest(form)).toEqual({
      root_cause: { summary: "boom", confidence: "low" },
      remediation_plan: { steps: [{ action: "restart", risk: "low" }] },
    });
  });

  it("carries scope, evidence, rollback, automatable and the source tag when set", () => {
    const form = validForm();
    form.remediation_plan.rollback = [{ action: "undo", command: "", risk: "medium" }];
    expect(formToRequest(form, ANALYSIS_SOURCE)).toEqual({
      root_cause: {
        summary: form.root_cause.summary,
        scope: "srv-victoria1:/var",
        evidence: ["df -h /var: 100% used"],
        confidence: "high",
      },
      remediation_plan: {
        steps: [
          { action: "Vacuum the journal", command: "journalctl --vacuum-size=200M", risk: "low" },
        ],
        rollback: [{ action: "undo", risk: "medium" }],
        automatable: true,
      },
      source: "snooze-web",
    });
  });

  it("does not alias the form's evidence array into the request body", () => {
    const form = validForm();
    const req = formToRequest(form);
    form.root_cause.evidence.push("added later");
    expect(req.root_cause.evidence).toEqual(["df -h /var: 100% used"]);
  });
});

describe("blank detection, against Go's strings.TrimSpace", () => {
  // The two validators must agree on what "empty" means or a save is rejected
  // by exactly one of them. JavaScript's trim() and Go's unicode.IsSpace
  // disagree on two code points, and both reach a text field by paste.
  it("treats U+0085 (NEL) as blank — Go does, String.prototype.trim does not", () => {
    const form = validForm();
    form.root_cause.summary = "\u0085";
    expect(validateAnalysisForm(form)).toEqual({ "root_cause.summary": "is required" });
  });

  it("does NOT treat U+FEFF as blank — JS trim does, Go does not", () => {
    const form = validForm();
    form.root_cause.summary = "﻿";
    expect(validateAnalysisForm(form)).toEqual({});
  });

  it("applies the same rule to an evidence item and a step action", () => {
    const form = validForm();
    form.root_cause.evidence = ["\u0085"];
    form.remediation_plan.steps = [{ action: "\u0085", command: "", risk: "low" }];
    expect(validateAnalysisForm(form)).toEqual({
      "root_cause.evidence[0]": "must not be empty",
      "remediation_plan.steps[0].action": "is required",
    });

    form.root_cause.evidence = ["﻿"];
    form.remediation_plan.steps = [{ action: "﻿", command: "", risk: "low" }];
    expect(validateAnalysisForm(form)).toEqual({});
  });
});

describe("toFieldErrors, on caller-controlled key names", () => {
  // The flat map is also fed straight from a server 422, whose `details` keys
  // echo paths a caller can influence. Walking them must never reach a
  // prototype.
  it("refuses to walk a __proto__ segment", () => {
    try {
      const tree = toFieldErrors({ "__proto__.polluted": "boom" });
      expect(({} as Record<string, unknown>)["polluted"]).toBeUndefined();
      expect(tree).toEqual({});
    } finally {
      delete (Object.prototype as unknown as Record<string, unknown>)["polluted"];
    }
  });

  it("refuses to walk constructor / prototype segments", () => {
    const tree = toFieldErrors({
      "constructor.prototype.polluted": "boom",
      "root_cause.summary": "is required",
    }) as unknown as Record<string, unknown>;
    // Inherited `constructor` is always there; what must not exist is an OWN
    // property the walk would have created on its way to a prototype.
    expect(Object.prototype.hasOwnProperty.call(tree, "constructor")).toBe(false);
    // The legitimate path in the same map still lands.
    expect((tree["root_cause"] as Record<string, { message: string }>)["summary"]?.message).toBe(
      "is required",
    );
  });
});

describe("analysisToForm, on non-conforming stored data", () => {
  // The subtree is stored as loose JSON and can be hand-edited in the DB. The
  // SPA has no ErrorBoundary, so a throw here takes down the whole /alerts
  // route rather than one pane.
  it("narrows a non-string summary instead of handing the resolver a number", () => {
    const form = analysisToForm({ root_cause: { summary: 5 } } as unknown as StoredAgentic);
    expect(form.root_cause.summary).toBe("");
    expect(() => validateAnalysisForm(form)).not.toThrow();
  });

  it("drops a string evidence field instead of spreading it into characters", () => {
    const form = analysisToForm({
      root_cause: { summary: "boom", evidence: "abc" },
    } as unknown as StoredAgentic);
    expect(form.root_cause.evidence).toEqual([]);
  });

  it("drops a non-array steps field instead of throwing on .map", () => {
    const form = analysisToForm({
      remediation_plan: { steps: "x", rollback: 7 },
    } as unknown as StoredAgentic);
    expect(form.remediation_plan.steps).toEqual([{ action: "", command: "", risk: "" }]);
    expect(form.remediation_plan.rollback).toEqual([]);
  });

  it("narrows a non-string command and a non-boolean automatable", () => {
    const form = analysisToForm({
      remediation_plan: { steps: [{ action: 1, command: 7, risk: "low" }], automatable: 1 },
    } as unknown as StoredAgentic);
    expect(form.remediation_plan.steps[0]).toEqual({ action: "", command: "", risk: "low" });
    expect(form.remediation_plan.automatable).toBe(false);
  });

  it("survives a step that is not an object at all", () => {
    const form = analysisToForm({
      remediation_plan: { steps: [null, "x"] },
    } as unknown as StoredAgentic);
    expect(form.remediation_plan.steps).toEqual([
      { action: "", command: "", risk: "" },
      { action: "", command: "", risk: "" },
    ]);
  });
});

describe("formToRequest, on non-conforming values", () => {
  it("never calls trim on a non-string", () => {
    const form = {
      root_cause: { summary: 5, scope: 7, evidence: "abc", confidence: "high" },
      remediation_plan: {
        steps: [{ action: 1, command: 7, risk: "low" }],
        rollback: "x",
        automatable: 1,
      },
    } as unknown as AnalysisForm;
    expect(() => formToRequest(form, ANALYSIS_SOURCE)).not.toThrow();
    expect(formToRequest(form, ANALYSIS_SOURCE)).toEqual({
      root_cause: { summary: "", confidence: "high" },
      remediation_plan: { steps: [{ action: "", risk: "low" }] },
      source: "snooze-web",
    });
  });

  it("tolerates a non-string source tag", () => {
    expect(() => formToRequest(validForm(), 5 as unknown as string)).not.toThrow();
  });
});
