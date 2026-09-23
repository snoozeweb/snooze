import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { TooltipProvider } from "@/shared/ui/Tooltip";
import { authStore } from "@/lib/auth/store";
import { mswServer } from "@/tests/msw/server";
import { AnalysisEditor, type Agentic } from "./AnalysisEditor";
import type { AgenticEnvelope } from "./api";

const STORED: Agentic = {
  root_cause: {
    summary: "systemd-journald filled /var with 4.2G of logs",
    scope: "srv-victoria1:/var",
    evidence: ["journalctl: 4.2G under /var/log/journal"],
    confidence: "medium",
  },
  remediation_plan: {
    steps: [
      {
        action: "Vacuum the journal to 500M",
        command: "journalctl --vacuum-size=500M",
        risk: "low",
      },
      { action: "Cap SystemMaxUse in journald.conf", risk: "medium" },
    ],
    rollback: [{ action: "Restore the previous journald.conf", risk: "low" }],
    automatable: true,
  },
  analysis: { at: "2026-09-21T10:00:00Z", by: "agent-bot", source: "alert-rca" },
};

const SAVED: AgenticEnvelope = { uid: "r1", agentic: STORED };

function loginWithPerms(perms: string[], sub = "tester") {
  const header = btoa(JSON.stringify({ alg: "HS256", typ: "JWT" }));
  const body = btoa(
    JSON.stringify({ sub, exp: Math.floor(Date.now() / 1000) + 3600, permissions: perms }),
  );
  authStore.getState().login(`${header}.${body}.sig`);
}

function makeWrapper() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={client}>
        <TooltipProvider>{children}</TooltipProvider>
      </QueryClientProvider>
    );
  };
}

type Handlers = {
  onSaved?: (envelope: AgenticEnvelope) => void;
  onCancel?: () => void;
};

function renderEditor(initial: Agentic | null, handlers: Handlers = {}) {
  return render(
    <AnalysisEditor
      uid="r1"
      initial={initial}
      onSaved={handlers.onSaved ?? (() => undefined)}
      onCancel={handlers.onCancel ?? (() => undefined)}
    />,
    { wrapper: makeWrapper() },
  );
}

/** Capture whatever the editor PUTs, and answer with the stored envelope. */
function stubSave(): { body: () => Record<string, unknown> | null } {
  let captured: Record<string, unknown> | null = null;
  mswServer.use(
    http.put("/api/v1/record/r1/agentic", async ({ request }) => {
      captured = (await request.json()) as Record<string, unknown>;
      return HttpResponse.json(SAVED);
    }),
  );
  return { body: () => captured };
}

function stepList(): HTMLElement {
  return screen.getByRole("list", { name: "Steps" });
}

describe("AnalysisEditor", () => {
  afterEach(() => {
    // Unmount BEFORE clearing the session: the editor subscribes to the auth
    // store, and vitest runs this hook ahead of the global cleanup, so a bare
    // logout would push a state update into a live tree outside act().
    cleanup();
    authStore.getState().logout({ revoke: false });
  });

  it("opens on the stored analysis, field for field", () => {
    loginWithPerms(["rw_protected"]);
    renderEditor(STORED);

    expect(screen.getByLabelText("Summary")).toHaveValue(
      "systemd-journald filled /var with 4.2G of logs",
    );
    expect(screen.getByLabelText("Scope")).toHaveValue("srv-victoria1:/var");
    expect(screen.getByLabelText("Evidence 1")).toHaveValue(
      "journalctl: 4.2G under /var/log/journal",
    );
    expect(screen.getByRole("radio", { name: "Medium" })).toBeChecked();

    const actions = within(stepList()).getAllByLabelText("Action");
    expect(actions).toHaveLength(2);
    expect(actions[0]).toHaveValue("Vacuum the journal to 500M");
    expect(actions[1]).toHaveValue("Cap SystemMaxUse in journald.conf");
    expect(within(stepList()).getAllByLabelText("Command")[0]).toHaveValue(
      "journalctl --vacuum-size=500M",
    );

    // A written rollback is shown expanded — only an absent one folds away.
    const rollback = screen.getByRole("list", { name: "Rollback" });
    expect(within(rollback).getByLabelText("Action")).toHaveValue(
      "Restore the previous journald.conf",
    );

    // Automatable is on and step 2 is medium risk — advisory, not an error.
    expect(
      screen.getByText(
        "Plan has medium or high-risk steps; the agent loop will not run it unattended.",
      ),
    ).toBeInTheDocument();
  });

  it("opens the fields the contract grew — detail, caveats, verdict, timing — on their stored values", () => {
    loginWithPerms(["rw_protected"]);
    renderEditor({
      ...STORED,
      root_cause: {
        ...STORED.root_cause!,
        detail: "The journal grew 4.2G in six hours after the upgrade.",
        caveats: ["Could not read the standby's journal"],
      },
      remediation_plan: {
        ...STORED.remediation_plan!,
        status: "action_required",
        steps: [
          { action: "Vacuum the journal to 500M", risk: "low", when: "now" },
          { action: "Add a retention alert", risk: "low", when: "follow_up" },
        ],
      },
    });

    expect(screen.getByLabelText("Detail")).toHaveValue(
      "The journal grew 4.2G in six hours after the upgrade.",
    );
    expect(screen.getByLabelText("Caveat 1")).toHaveValue("Could not read the standby's journal");
    expect(screen.getByRole("combobox", { name: /^Status/ })).toHaveTextContent("Action required");
    const whens = within(stepList()).getAllByRole("combobox", { name: /^When/ });
    expect(whens.map((w) => w.textContent)).toEqual(["Now", "Follow-up"]);
    // The summary hint says what the field is FOR, now that it has a sibling.
    expect(screen.getByText(/put the explanation in Detail/i)).toBeInTheDocument();
  });

  it("does not offer a timing on rollback steps, which have no 'now' of their own", () => {
    loginWithPerms(["rw_protected"]);
    renderEditor(STORED);
    const rollback = screen.getByRole("list", { name: "Rollback" });
    expect(within(rollback).queryByRole("combobox", { name: /^When/ })).toBeNull();
  });

  it("PUTs detail, caveats, the verdict and a step's timing, and omits them when blank", async () => {
    const user = userEvent.setup();
    loginWithPerms(["rw_protected"]);
    const save = stubSave();
    renderEditor(STORED);

    await user.type(screen.getByLabelText("Detail"), "Grew after the upgrade.");
    await user.click(screen.getByRole("button", { name: "Add caveat" }));
    await user.type(screen.getByLabelText("Caveat 1"), "Standby not checked");
    await user.click(screen.getByRole("combobox", { name: /^Status/ }));
    await user.click(screen.getByRole("option", { name: "Self-resolved" }));
    await user.click(within(stepList()).getAllByRole("combobox", { name: /^When/ })[0]!);
    await user.click(screen.getByRole("option", { name: "Follow-up" }));

    await user.click(screen.getByRole("button", { name: "Save analysis" }));
    await waitFor(() => expect(save.body()).not.toBeNull());
    const body = save.body() as {
      root_cause: Record<string, unknown>;
      remediation_plan: { status?: string; steps: Record<string, unknown>[] };
    };
    expect(body.root_cause["detail"]).toBe("Grew after the upgrade.");
    expect(body.root_cause["caveats"]).toEqual(["Standby not checked"]);
    expect(body.remediation_plan.status).toBe("self_resolved");
    expect(body.remediation_plan.steps[0]?.["when"]).toBe("follow_up");
    // The step nobody gave a timing to carries no key at all.
    expect(body.remediation_plan.steps[1]).not.toHaveProperty("when");
  });

  it("lands a server 422 on the new fields too", async () => {
    const user = userEvent.setup();
    loginWithPerms(["rw_protected"]);
    mswServer.use(
      http.put("/api/v1/record/r1/agentic", () =>
        HttpResponse.json(
          {
            error: {
              code: "unprocessable",
              message: "agentic analysis is invalid",
              details: {
                "root_cause.detail": "must be at most 2000 characters",
                "root_cause.caveats[0]": "must be at most 300 characters",
                "remediation_plan.status":
                  "must be one of action_required|self_resolved|monitoring|resolved",
                "remediation_plan.steps[0].when": "must be one of now|follow_up",
              },
            },
          },
          { status: 422 },
        ),
      ),
    );
    renderEditor({
      ...STORED,
      root_cause: { ...STORED.root_cause!, caveats: ["too long, per the server"] },
    });

    await user.click(screen.getByRole("button", { name: "Save analysis" }));
    expect(await screen.findByText("Detail must be at most 2000 characters.")).toBeInTheDocument();
    expect(screen.getByText("Caveat 1 must be at most 300 characters.")).toBeInTheDocument();
    expect(
      screen.getByText("Status must be one of action_required|self_resolved|monitoring|resolved."),
    ).toBeInTheDocument();
    expect(screen.getByText("When must be one of now|follow_up.")).toBeInTheDocument();
    // Every path landed on a field, so nothing is bannered as unplaceable.
    expect(screen.queryByText(/The server rejected the payload/)).toBeNull();
  });

  it("starts an author-from-scratch form on exactly one blank step", () => {
    loginWithPerms(["rw_protected"]);
    renderEditor(null);

    expect(screen.getByLabelText("Summary")).toHaveValue("");
    // No confidence is pre-picked: the author states one instead of inheriting
    // a guess.
    for (const level of ["High", "Medium", "Low"]) {
      expect(screen.getByRole("radio", { name: level })).not.toBeChecked();
    }
    const actions = within(stepList()).getAllByLabelText("Action");
    expect(actions).toHaveLength(1);
    expect(actions[0]).toHaveValue("");
    // The rollback starts empty and folded away.
    expect(screen.queryByRole("list", { name: "Rollback" })).toBeNull();
  });

  it("puts the caret in the step it just added", async () => {
    const user = userEvent.setup();
    loginWithPerms(["rw_protected"]);
    renderEditor(null);

    await user.click(screen.getByRole("button", { name: "Add step" }));
    const actions = within(stepList()).getAllByLabelText("Action");
    expect(actions).toHaveLength(2);
    await waitFor(() => expect(actions[1]).toHaveFocus());
  });

  it("refuses to remove the last step, and says why", () => {
    loginWithPerms(["rw_protected"]);
    renderEditor(null);

    const remove = within(stepList()).getByRole("button", { name: "Remove step 1" });
    expect(remove).toBeDisabled();
    expect(remove).toHaveAttribute("title", "A plan needs at least one step");
  });

  it("reorders with the keyboard buttons, and the PUT carries the new order", async () => {
    const user = userEvent.setup();
    loginWithPerms(["rw_protected"]);
    const save = stubSave();
    renderEditor(STORED);

    await user.click(within(stepList()).getByRole("button", { name: "Move step 1 down" }));
    const reordered = within(stepList()).getAllByLabelText("Action");
    expect(reordered[0]).toHaveValue("Cap SystemMaxUse in journald.conf");
    expect(reordered[1]).toHaveValue("Vacuum the journal to 500M");

    await user.click(screen.getByRole("button", { name: "Save analysis" }));
    await waitFor(() => expect(save.body()).not.toBeNull());
    const plan = save.body()?.["remediation_plan"] as { steps: { action: string }[] };
    expect(plan.steps.map((s) => s.action)).toEqual([
      "Cap SystemMaxUse in journald.conf",
      "Vacuum the journal to 500M",
    ]);
  });

  it("stamps the web UI as the source of everything it writes", async () => {
    const user = userEvent.setup();
    loginWithPerms(["rw_protected"]);
    const save = stubSave();
    const onSaved = vi.fn();
    renderEditor(STORED, { onSaved });

    await user.click(screen.getByRole("button", { name: "Save analysis" }));
    await waitFor(() => expect(onSaved).toHaveBeenCalledTimes(1));
    expect(save.body()?.["source"]).toBe("snooze-web");
  });

  it("refuses an incomplete form locally, on the fields that are incomplete", async () => {
    const user = userEvent.setup();
    loginWithPerms(["rw_protected"]);
    let puts = 0;
    mswServer.use(
      http.put("/api/v1/record/r1/agentic", () => {
        puts += 1;
        return HttpResponse.json(SAVED);
      }),
    );
    renderEditor(null);

    await user.click(screen.getByRole("button", { name: "Save analysis" }));
    expect(await screen.findByText("Summary is required.")).toBeInTheDocument();
    expect(screen.getByText("Confidence is required (high|medium|low).")).toBeInTheDocument();
    expect(screen.getByText("Action is required.")).toBeInTheDocument();
    expect(screen.getByText("Risk is required (low|medium|high).")).toBeInTheDocument();
    expect(puts).toBe(0);
  });

  it("reports an over-long plan on the list, not on twenty-one rows", async () => {
    const user = userEvent.setup();
    loginWithPerms(["rw_protected"]);
    const overlong: Agentic = {
      ...STORED,
      remediation_plan: {
        steps: Array.from({ length: 21 }, (_, i) => ({
          action: `Step ${i + 1}`,
          risk: "low" as const,
        })),
      },
    };
    renderEditor(overlong);

    await user.click(screen.getByRole("button", { name: "Save analysis" }));
    expect(
      await screen.findByText("The steps list must hold at most 20 steps."),
    ).toBeInTheDocument();
    // The Add button is already at the cap, and says so.
    expect(screen.getByRole("button", { name: "Add step" })).toBeDisabled();
  });

  it("lands a server 422's per-field messages on those fields", async () => {
    const user = userEvent.setup();
    loginWithPerms(["rw_protected"]);
    mswServer.use(
      http.put("/api/v1/record/r1/agentic", () =>
        HttpResponse.json(
          {
            error: {
              code: "unprocessable",
              message: "agentic analysis is invalid",
              details: {
                "root_cause.summary": "must be at most 500 characters",
                "remediation_plan.steps[1].risk": "must be one of low|medium|high",
              },
            },
          },
          { status: 422 },
        ),
      ),
    );
    renderEditor(STORED);

    await user.click(screen.getByRole("button", { name: "Save analysis" }));
    expect(await screen.findByText("Summary must be at most 500 characters.")).toBeInTheDocument();
    expect(screen.getByText("Risk must be one of low|medium|high.")).toBeInTheDocument();
  });

  it("keeps the form editable when the session lost rw_protected", async () => {
    const user = userEvent.setup();
    loginWithPerms(["rw_protected"]);
    mswServer.use(
      http.put("/api/v1/record/r1/agentic", () =>
        HttpResponse.json(
          { error: { code: "forbidden", message: "rw_protected required" } },
          { status: 403 },
        ),
      ),
    );
    renderEditor(STORED);

    await user.click(screen.getByRole("button", { name: "Save analysis" }));
    expect(
      await screen.findByText(
        "Your session no longer holds rw_protected. Ask an admin to grant it, then retry.",
      ),
    ).toBeInTheDocument();
    // Nothing was reset or disabled — the fix is a grant elsewhere, then Save.
    expect(screen.getByLabelText("Summary")).toHaveValue(
      "systemd-journald filled /var with 4.2G of logs",
    );
    expect(screen.getByRole("button", { name: "Save analysis" })).toBeEnabled();
  });

  it("names a deleted alert rather than blaming the form", async () => {
    const user = userEvent.setup();
    loginWithPerms(["rw_protected"]);
    mswServer.use(
      http.put("/api/v1/record/r1/agentic", () =>
        HttpResponse.json(
          { error: { code: "not_found", message: "no such record" } },
          { status: 404 },
        ),
      ),
    );
    renderEditor(STORED);

    await user.click(screen.getByRole("button", { name: "Save analysis" }));
    expect(await screen.findByText("This alert no longer exists.")).toBeInTheDocument();
  });

  it("asks before throwing away unsaved edits, and leaves cleanly when there are none", async () => {
    const user = userEvent.setup();
    loginWithPerms(["rw_protected"]);
    const onCancel = vi.fn();
    renderEditor(STORED, { onCancel });

    // Untouched: Cancel just leaves.
    await user.click(screen.getByRole("button", { name: "Cancel" }));
    expect(onCancel).toHaveBeenCalledTimes(1);

    await user.type(screen.getByLabelText("Summary"), " and then some");
    await user.click(screen.getByRole("button", { name: "Cancel" }));
    expect(await screen.findByText("Discard changes?")).toBeInTheDocument();
    expect(onCancel).toHaveBeenCalledTimes(1);

    await user.click(screen.getByRole("button", { name: "Discard changes" }));
    await waitFor(() => expect(onCancel).toHaveBeenCalledTimes(2));
  });

  it("warns that saving replaces someone else's analysis", () => {
    loginWithPerms(["rw_protected"], "carol");
    renderEditor(STORED);
    expect(
      screen.getByText("Replaces the analysis written by alert-rca; provenance will show you."),
    ).toBeInTheDocument();
  });

  it("banners a 422 whose path no control can show, instead of failing silently", async () => {
    const user = userEvent.setup();
    loginWithPerms(["rw_protected"]);
    // "$" is the server's body path (trailing data, an unattributable type
    // error). No field carries it, so setError would write into a branch of
    // the error tree nothing renders.
    mswServer.use(
      http.put("/api/v1/record/r1/agentic", () =>
        HttpResponse.json(
          {
            error: {
              code: "unprocessable",
              message: "agentic analysis is invalid",
              details: { $: "unexpected data after the JSON object" },
            },
          },
          { status: 422 },
        ),
      ),
    );
    renderEditor(STORED);

    await user.click(screen.getByRole("button", { name: "Save analysis" }));
    expect(
      await screen.findByText(
        "The server rejected the payload: $: unexpected data after the JSON object",
      ),
    ).toBeInTheDocument();
  });

  it("banners a 422 on `source`, which the form has no control for by design", async () => {
    const user = userEvent.setup();
    loginWithPerms(["rw_protected"]);
    mswServer.use(
      http.put("/api/v1/record/r1/agentic", () =>
        HttpResponse.json(
          {
            error: {
              code: "unprocessable",
              message: "agentic analysis is invalid",
              details: { source: "must be at most 64 characters" },
            },
          },
          { status: 422 },
        ),
      ),
    );
    renderEditor(STORED);

    await user.click(screen.getByRole("button", { name: "Save analysis" }));
    expect(
      await screen.findByText(
        "The server rejected the payload: source: must be at most 64 characters",
      ),
    ).toBeInTheDocument();
  });

  it("places what it can and banners the rest when a 422 mixes known and unknown paths", async () => {
    const user = userEvent.setup();
    loginWithPerms(["rw_protected"]);
    mswServer.use(
      http.put("/api/v1/record/r1/agentic", () =>
        HttpResponse.json(
          {
            error: {
              code: "unprocessable",
              message: "agentic analysis is invalid",
              details: {
                "root_cause.summary": "is required",
                // A field this client build has never heard of — the server is
                // ahead of the SPA.
                "root_cause.blast_radius": "is required",
              },
            },
          },
          { status: 422 },
        ),
      ),
    );
    renderEditor(STORED);

    await user.click(screen.getByRole("button", { name: "Save analysis" }));
    expect(await screen.findByText("Summary is required.")).toBeInTheDocument();
    expect(
      screen.getByText("The server rejected the payload: root_cause.blast_radius: is required"),
    ).toBeInTheDocument();
  });

  it("does not banner a 422 every one of whose paths lands on a field", async () => {
    const user = userEvent.setup();
    loginWithPerms(["rw_protected"]);
    mswServer.use(
      http.put("/api/v1/record/r1/agentic", () =>
        HttpResponse.json(
          {
            error: {
              code: "unprocessable",
              message: "agentic analysis is invalid",
              details: { "root_cause.summary": "is required" },
            },
          },
          { status: 422 },
        ),
      ),
    );
    renderEditor(STORED);

    await user.click(screen.getByRole("button", { name: "Save analysis" }));
    expect(await screen.findByText("Summary is required.")).toBeInTheDocument();
    expect(screen.queryByText(/The server rejected the payload/)).toBeNull();
  });

  it("does not mine a non-422 for field paths", async () => {
    const user = userEvent.setup();
    loginWithPerms(["rw_protected"]);
    mswServer.use(
      http.put("/api/v1/record/r1/agentic", () =>
        HttpResponse.json(
          {
            error: {
              code: "conflict",
              message: "the record moved",
              details: { "root_cause.summary": "is required" },
            },
          },
          { status: 409 },
        ),
      ),
    );
    renderEditor(STORED);

    await user.click(screen.getByRole("button", { name: "Save analysis" }));
    expect(await screen.findByText("The record moved.")).toBeInTheDocument();
    expect(screen.queryByText("Summary is required.")).toBeNull();
  });

  it("puts the caret in the evidence line it just added", async () => {
    const user = userEvent.setup();
    loginWithPerms(["rw_protected"]);
    renderEditor(STORED);

    await user.click(screen.getByRole("button", { name: "Add evidence" }));
    await waitFor(() => expect(screen.getByLabelText("Evidence 2")).toHaveFocus());
  });

  it("moves the caret to the row that took the removed evidence's place", async () => {
    const user = userEvent.setup();
    loginWithPerms(["rw_protected"]);
    const withThree: Agentic = {
      ...STORED,
      root_cause: {
        summary: "systemd-journald filled /var with 4.2G of logs",
        scope: "srv-victoria1:/var",
        confidence: "medium",
        evidence: ["one", "two", "three"],
      },
    };
    renderEditor(withThree);

    await user.click(screen.getByRole("button", { name: "Remove evidence 2" }));
    await waitFor(() => expect(screen.getByLabelText("Evidence 2")).toHaveFocus());
    expect(screen.getByLabelText("Evidence 2")).toHaveValue("three");
  });

  it("falls back to Add evidence when the last line is removed", async () => {
    const user = userEvent.setup();
    loginWithPerms(["rw_protected"]);
    renderEditor(STORED);

    await user.click(screen.getByRole("button", { name: "Remove evidence 1" }));
    await waitFor(() => expect(screen.getByRole("button", { name: "Add evidence" })).toHaveFocus());
  });

  it("moves the caret to the step that took the removed step's place", async () => {
    const user = userEvent.setup();
    loginWithPerms(["rw_protected"]);
    renderEditor(STORED);

    await user.click(within(stepList()).getByRole("button", { name: "Remove step 1" }));
    await waitFor(() => expect(within(stepList()).getAllByLabelText("Action")[0]).toHaveFocus());
    expect(within(stepList()).getAllByLabelText("Action")[0]).toHaveValue(
      "Cap SystemMaxUse in journald.conf",
    );
  });

  it("falls back to the add button when the last rollback step is removed", async () => {
    const user = userEvent.setup();
    loginWithPerms(["rw_protected"]);
    renderEditor(STORED);

    const rollback = screen.getByRole("list", { name: "Rollback" });
    await user.click(within(rollback).getByRole("button", { name: "Remove rollback step 1" }));
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "Add rollback step" })).toHaveFocus(),
    );
  });

  it("says nothing about provenance when you are correcting your own", () => {
    loginWithPerms(["rw_protected"], "agent-bot");
    renderEditor(STORED);
    expect(screen.queryByText(/Replaces the analysis written by/)).toBeNull();
  });
});
