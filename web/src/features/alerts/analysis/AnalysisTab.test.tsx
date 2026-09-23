import { act, cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { afterEach, describe, expect, it } from "vitest";
import type { ReactNode } from "react";
import { TooltipProvider } from "@/shared/ui/Tooltip";
import { authStore } from "@/lib/auth/store";
import { mswServer } from "@/tests/msw/server";
import { AnalysisEmpty } from "./AnalysisEmpty";
import { AnalysisTab } from "./AnalysisTab";
import type { AgenticEnvelope } from "./api";

const ENVELOPE: AgenticEnvelope = {
  uid: "r1",
  agentic: {
    root_cause: {
      summary: "systemd-journald filled /var with 4.2G of logs",
      scope: "srv-victoria1:/var",
      evidence: ["journalctl: 4.2G under /var/log/journal", "the unit restarted twice"],
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
  },
};

function loginWithPerms(perms: string[]) {
  const header = btoa(JSON.stringify({ alg: "HS256", typ: "JWT" }));
  const body = btoa(
    JSON.stringify({
      sub: "tester",
      exp: Math.floor(Date.now() / 1000) + 3600,
      permissions: perms,
    }),
  );
  authStore.getState().login(`${header}.${body}.sig`);
}

/** Never-resolving GET, so the pending branch stays on screen to be asserted. */
function stubPending() {
  mswServer.use(http.get("/api/v1/record/r1/agentic", () => new Promise(() => undefined)));
}

function stubAnalysis(envelope: AgenticEnvelope) {
  mswServer.use(http.get("/api/v1/record/r1/agentic", () => HttpResponse.json(envelope)));
}

function stubMissing() {
  mswServer.use(
    http.get("/api/v1/record/r1/agentic", () =>
      HttpResponse.json(
        { error: { code: "not_found", message: "record carries no analysis" } },
        { status: 404 },
      ),
    ),
  );
}

/**
 * One QueryClient per render — a client shared across tests would serve the
 * previous test's cached envelope to the next one.
 */
function makeWrapper() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={client}>
        {/* ProvenanceLine's TimeCell renders a Tooltip, which needs a provider
            ancestor (app-wide in production). */}
        <TooltipProvider>{children}</TooltipProvider>
      </QueryClientProvider>
    );
  };
}

function renderTab(uid: string | undefined, lastEpoch?: number) {
  return render(<AnalysisTab uid={uid} {...(lastEpoch !== undefined ? { lastEpoch } : {})} />, {
    wrapper: makeWrapper(),
  });
}

/** The ⋯ menu beside Edit, where Remove lives. */
async function openMoreMenu(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole("button", { name: "More analysis actions" }));
}

/**
 * Same wrapper, but the QueryClient is handed back so a test can drive a
 * *background refetch* — the thing that turns a loaded pane into a failing one
 * while the operator is mid-edit.
 */
function renderTabWithClient(uid: string | undefined) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const view = render(
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <AnalysisTab uid={uid} />
      </TooltipProvider>
    </QueryClientProvider>,
  );
  return { ...view, client };
}

describe("AnalysisTab", () => {
  afterEach(() => {
    // Unmount BEFORE clearing the session: these components subscribe to the
    // auth store, and vitest runs this hook ahead of the global cleanup, so a
    // bare logout would push a state update into a live tree outside act().
    cleanup();
    authStore.getState().logout({ revoke: false });
  });

  it("shows a skeleton while the analysis is in flight", () => {
    loginWithPerms(["ro_record"]);
    stubPending();
    renderTab("r1");
    expect(screen.getAllByTestId("skeleton").length).toBeGreaterThan(0);
  });

  it("never fires a request — or waits on one — for a row with no uid", async () => {
    loginWithPerms(["ro_record"]);
    let hits = 0;
    mswServer.use(
      http.get("/api/v1/record/:uid/agentic", () => {
        hits += 1;
        return HttpResponse.json(ENVELOPE);
      }),
    );
    renderTab(undefined);
    expect(await screen.findByText("No analysis yet")).toBeInTheDocument();
    expect(hits).toBe(0);
  });

  it("renders the empty state on a 404 — an unanalysed alert is normal, not broken", async () => {
    loginWithPerms(["ro_record"]);
    stubMissing();
    renderTab("r1");
    expect(await screen.findByText("No analysis yet")).toBeInTheDocument();
    expect(
      screen.getByText(
        "Analyses are written by the alert-rca agent, or by anyone allowed to edit analyses.",
      ),
    ).toBeInTheDocument();
    // The permission's wire name is not something a reader should have to know.
    expect(screen.queryByText(/rw_protected/)).toBeNull();
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("turns 'Write analysis' into the editor, on a blank form", async () => {
    const user = userEvent.setup();
    loginWithPerms(["ro_record", "rw_protected"]);
    stubMissing();
    renderTab("r1");

    await user.click(await screen.findByRole("button", { name: "Write analysis" }));
    expect(screen.getByText("Editing")).toBeInTheDocument();
    // The editor is a lazy chunk — it arrives a tick after the mode flips.
    expect(await screen.findByLabelText("Summary")).toHaveValue("");
    expect(screen.getByRole("button", { name: "Save analysis" })).toBeInTheDocument();
  });

  it("withholds the write affordance from a session that only holds rw_all", async () => {
    // The server's check on this route is literal membership; a wildcard admin
    // offered the button would fill the form and then eat a 403.
    loginWithPerms(["rw_all"]);
    stubMissing();
    renderTab("r1");
    expect(await screen.findByText("No analysis yet")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Write analysis" })).toBeNull();
  });

  it("offers 'Write analysis' only to a holder of the literal rw_protected", () => {
    // The button lives on AnalysisEmpty, which takes the handler T2 will pass;
    // both halves of the gate (handler AND permission) are checked here.
    // Each render is unmounted before the session changes under it: the
    // component subscribes to the auth store, so a logout while it is mounted
    // pushes a state update outside act().
    loginWithPerms(["ro_record", "rw_protected"]);
    const granted = render(<AnalysisEmpty onWrite={() => undefined} />, {
      wrapper: makeWrapper(),
    });
    expect(screen.getByRole("button", { name: "Write analysis" })).toBeInTheDocument();
    granted.unmount();

    authStore.getState().logout({ revoke: false });
    // rw_all does NOT stand in for rw_protected — the server's check is literal.
    loginWithPerms(["rw_all"]);
    const wildcard = render(<AnalysisEmpty onWrite={() => undefined} />, {
      wrapper: makeWrapper(),
    });
    expect(screen.queryByRole("button", { name: "Write analysis" })).toBeNull();
    wildcard.unmount();
  });

  it("renders the verdict, the plan, the evidence and the provenance", async () => {
    const user = userEvent.setup();
    loginWithPerms(["ro_record"]);
    stubAnalysis(ENVELOPE);
    renderTab("r1");
    // The verdict: the headline and the scope chip.
    expect(
      await screen.findByRole("heading", {
        name: "systemd-journald filled /var with 4.2G of logs",
      }),
    ).toBeInTheDocument();
    expect(screen.getByText("srv-victoria1:/var")).toBeInTheDocument();

    // The plan: both steps, only the risk worth a look, the command, the
    // automatable mark in the heading, and the rollback folded away.
    expect(screen.getByRole("heading", { name: /What to do/ })).toBeInTheDocument();
    expect(screen.getByText("Vacuum the journal to 500M")).toBeInTheDocument();
    expect(screen.getByText("Cap SystemMaxUse in journald.conf")).toBeInTheDocument();
    expect(screen.getByText("journalctl --vacuum-size=500M")).toBeInTheDocument();
    expect(screen.queryByText("Low risk")).toBeNull();
    expect(screen.getByText("Medium risk")).toBeInTheDocument();
    expect(screen.getByText("Automatable")).toBeInTheDocument();
    expect(screen.queryByText(/Automatable:/)).toBeNull();
    expect(screen.getByRole("button", { name: /Rollback/ })).toHaveAttribute(
      "aria-expanded",
      "false",
    );

    // Evidence is support, so it waits behind a disclosure that says how much.
    const evidence = screen.getByRole("button", { name: "Evidence · 2" });
    expect(evidence).toHaveAttribute("aria-expanded", "false");
    expect(screen.queryByText(/4.2G under \/var\/log\/journal/)).toBeNull();
    await user.click(evidence);
    expect(evidence).toHaveAttribute("aria-expanded", "true");
    expect(screen.getByText(/4.2G under \/var\/log\/journal/)).toBeInTheDocument();

    // Provenance: said once, and said to be a model's.
    expect(screen.getAllByText("Medium confidence")).toHaveLength(1);
    expect(screen.getByText("AI analysis")).toBeInTheDocument();
    expect(screen.getByText("alert-rca")).toBeInTheDocument();
    expect(screen.getByText("agent-bot")).toBeInTheDocument();
  });

  it("reads verdict → plan → caveats → evidence, top to bottom", async () => {
    loginWithPerms(["ro_record"]);
    stubAnalysis({
      ...ENVELOPE,
      agentic: {
        ...ENVELOPE.agentic,
        root_cause: {
          ...ENVELOPE.agentic.root_cause!,
          caveats: ["Could not read the journal on the standby"],
        },
      },
    });
    renderTab("r1");
    const headline = await screen.findByRole("heading", {
      name: "systemd-journald filled /var with 4.2G of logs",
    });
    const plan = screen.getByRole("heading", { name: /What to do/ });
    const caveats = screen.getByRole("heading", { name: "Caveats" });
    const evidence = screen.getByRole("button", { name: /^Evidence/ });
    const follows = (a: Element, b: Element) =>
      Boolean(a.compareDocumentPosition(b) & Node.DOCUMENT_POSITION_FOLLOWING);
    expect(follows(headline, plan)).toBe(true);
    expect(follows(plan, caveats)).toBe(true);
    expect(follows(caveats, evidence)).toBe(true);
  });

  it("lifts a legacy 'caveat:' evidence line into Caveats, out of the evidence count", async () => {
    // The prod analysis filed its only caveat as evidence #7, where it was set
    // in code font at the bottom of a list nobody reads first.
    loginWithPerms(["ro_record"]);
    stubAnalysis({
      ...ENVELOPE,
      agentic: {
        ...ENVELOPE.agentic,
        root_cause: {
          ...ENVELOPE.agentic.root_cause!,
          evidence: [
            "journalctl: 4.2G under /var/log/journal",
            "caveat: the standby was not reachable over SSH",
          ],
        },
      },
    });
    renderTab("r1");
    expect(await screen.findByRole("heading", { name: "Caveats" })).toBeInTheDocument();
    expect(screen.getByText("the standby was not reachable over SSH")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Evidence · 1" })).toBeInTheDocument();
  });

  it("shows no Caveats section when there are none", async () => {
    loginWithPerms(["ro_record"]);
    stubAnalysis(ENVELOPE);
    renderTab("r1");
    await screen.findByRole("heading", { name: /What to do/ });
    expect(screen.queryByRole("heading", { name: "Caveats" })).toBeNull();
  });

  it("credits a person for an analysis saved from the web UI", async () => {
    loginWithPerms(["ro_record"]);
    stubAnalysis({
      ...ENVELOPE,
      agentic: {
        ...ENVELOPE.agentic,
        analysis: { at: "2026-09-21T10:00:00Z", by: "alice", source: "snooze-web" },
      },
    });
    renderTab("r1");
    expect(await screen.findByText(/Written by/)).toHaveTextContent("Written by alice");
    expect(screen.queryByText("AI analysis")).toBeNull();
  });

  it("warns — quietly — when the alert fired again after the analysis was written", async () => {
    loginWithPerms(["ro_record"]);
    stubAnalysis(ENVELOPE);
    // Written 10:00Z; the alert last fired an hour later.
    renderTab("r1", Math.floor(Date.UTC(2026, 8, 21, 11) / 1000));
    expect(
      await screen.findByText(
        "This alert fired again after the analysis was written — it may describe an earlier occurrence.",
      ),
    ).toBeInTheDocument();
    // A notice, not an alarm: nothing here is a failure.
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("says nothing about freshness when the alert has not fired since", async () => {
    loginWithPerms(["ro_record"]);
    stubAnalysis(ENVELOPE);
    renderTab("r1", Math.floor(Date.UTC(2026, 8, 21, 9) / 1000));
    await screen.findByRole("heading", { name: /What to do/ });
    expect(screen.queryByText(/fired again after the analysis/)).toBeNull();
  });

  it("puts Edit and a ⋯ menu holding Remove in the actions slot for an rw_protected holder", async () => {
    const user = userEvent.setup();
    loginWithPerms(["ro_record", "rw_protected"]);
    stubAnalysis(ENVELOPE);
    const { container } = renderTab("r1");
    await screen.findByRole("heading", { name: /What to do/ });
    const slot = container.querySelector('[data-slot="analysis-actions"]');
    expect(slot).not.toBeNull();
    expect(within(slot as HTMLElement).getByRole("button", { name: "Edit" })).toBeInTheDocument();
    // No destructive button sits in the trust row any more…
    expect(within(slot as HTMLElement).queryByRole("button", { name: "Remove" })).toBeNull();
    // …it waits one deliberate step away.
    await openMoreMenu(user);
    expect(screen.getByRole("menuitem", { name: "Remove" })).toBeInTheDocument();
  });

  it("shows an rw_all-only session the analysis, and no way to change it", async () => {
    loginWithPerms(["rw_all"]);
    stubAnalysis(ENVELOPE);
    renderTab("r1");
    await screen.findByRole("heading", { name: /What to do/ });
    expect(screen.queryByRole("button", { name: "Edit" })).toBeNull();
    expect(screen.queryByRole("button", { name: "More analysis actions" })).toBeNull();
  });

  it("swaps the read views for the editor, and hides Edit/Remove while editing", async () => {
    const user = userEvent.setup();
    loginWithPerms(["ro_record", "rw_protected"]);
    stubAnalysis(ENVELOPE);
    renderTab("r1");
    await screen.findByRole("heading", { name: /What to do/ });

    await user.click(screen.getByRole("button", { name: "Edit" }));
    expect(screen.getByText("Editing")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Edit" })).toBeNull();
    expect(screen.queryByRole("button", { name: "More analysis actions" })).toBeNull();
    // The read views are gone; the form holds the same analysis.
    expect(screen.queryByRole("heading", { name: /What to do/ })).toBeNull();
    expect(await screen.findByLabelText("Summary")).toHaveValue(
      "systemd-journald filled /var with 4.2G of logs",
    );

    // Cancel on an untouched form goes straight back to reading.
    await user.click(screen.getByRole("button", { name: "Cancel" }));
    expect(await screen.findByRole("heading", { name: /What to do/ })).toBeInTheDocument();
  });

  it("returns to the empty state once Remove is confirmed", async () => {
    const user = userEvent.setup();
    loginWithPerms(["ro_record", "rw_protected"]);
    let cleared = false;
    mswServer.use(
      http.get("/api/v1/record/r1/agentic", () =>
        cleared
          ? HttpResponse.json(
              { error: { code: "not_found", message: "record carries no analysis" } },
              { status: 404 },
            )
          : HttpResponse.json(ENVELOPE),
      ),
      http.delete("/api/v1/record/r1/agentic", () => {
        cleared = true;
        return new HttpResponse(null, { status: 204 });
      }),
    );
    renderTab("r1");
    await screen.findByRole("heading", { name: /What to do/ });

    await openMoreMenu(user);
    await user.click(screen.getByRole("menuitem", { name: "Remove" }));
    // The menu item only asks; the dialog is still what removes.
    await user.click(await screen.findByRole("button", { name: "Remove analysis" }));
    expect(await screen.findByText("No analysis yet")).toBeInTheDocument();
  });

  it("says 'Manual' when the plan is not automatable", async () => {
    loginWithPerms(["ro_record"]);
    stubAnalysis({
      ...ENVELOPE,
      agentic: {
        ...ENVELOPE.agentic,
        remediation_plan: { steps: [{ action: "Page the DBA", risk: "high" }] },
      },
    });
    renderTab("r1");
    expect(await screen.findByText("Manual")).toBeInTheDocument();
    expect(screen.queryByText("Automatable")).toBeNull();
    expect(screen.queryByRole("button", { name: /Rollback/ })).toBeNull();
  });

  it("returns to reading when the tab is pointed at another alert mid-edit", async () => {
    const user = userEvent.setup();
    loginWithPerms(["ro_record", "rw_protected"]);
    mswServer.use(
      http.get("/api/v1/record/:uid/agentic", ({ params }) =>
        HttpResponse.json({ ...ENVELOPE, uid: params["uid"] as string }),
      ),
    );
    const { rerender } = renderTab("r1");
    await screen.findByRole("heading", { name: /What to do/ });
    await user.click(screen.getByRole("button", { name: "Edit" }));
    expect(await screen.findByLabelText("Summary")).toBeInTheDocument();

    // Prev/next retargets the inspector. `uid` is read fresh at submit time,
    // so an editor left open over the new alert would PUT the old alert's
    // draft onto it.
    rerender(<AnalysisTab uid="r2" />);

    expect(await screen.findByRole("heading", { name: /What to do/ })).toBeInTheDocument();
    expect(screen.queryByText("Editing")).toBeNull();
    expect(screen.queryByLabelText("Summary")).toBeNull();
  });

  it("keeps the editor on screen when a background refetch fails", async () => {
    const user = userEvent.setup();
    loginWithPerms(["ro_record", "rw_protected"]);
    stubAnalysis(ENVELOPE);
    const { client } = renderTabWithClient("r1");
    await screen.findByRole("heading", { name: /What to do/ });
    await user.click(screen.getByRole("button", { name: "Edit" }));
    await screen.findByLabelText("Summary");

    // The poll behind the pane 500s. The operator's half-written correction is
    // not something an error panel gets to throw away.
    mswServer.use(
      http.get("/api/v1/record/r1/agentic", () =>
        HttpResponse.json(
          { error: { code: "internal", message: "backend is having a moment" } },
          { status: 500 },
        ),
      ),
    );
    await act(async () => {
      await client.refetchQueries({ queryKey: ["agentic", "r1"] });
    });

    // The failure is reported beside the form, not instead of it.
    expect(await screen.findByRole("alert")).toHaveTextContent(/backend is having a moment/i);
    expect(screen.getByLabelText("Summary")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Save analysis" })).toBeInTheDocument();
  });

  it("reports a failed refresh beside the analysis rather than replacing it", async () => {
    loginWithPerms(["ro_record"]);
    stubAnalysis(ENVELOPE);
    const { client } = renderTabWithClient("r1");
    await screen.findByRole("heading", { name: /What to do/ });

    mswServer.use(
      http.get("/api/v1/record/r1/agentic", () =>
        HttpResponse.json(
          { error: { code: "internal", message: "backend is having a moment" } },
          { status: 500 },
        ),
      ),
    );
    await act(async () => {
      await client.refetchQueries({ queryKey: ["agentic", "r1"] });
    });

    expect(await screen.findByRole("alert")).toHaveTextContent(/backend is having a moment/i);
    expect(screen.getByRole("heading", { name: /What to do/ })).toBeInTheDocument();
  });

  it("drops the confidence chip when the stored value is not one of the three", async () => {
    loginWithPerms(["ro_record"]);
    // A hand-written document, or one from a server that knows a level this
    // bundle does not — the cast is the point of the test, so it names the
    // shape rather than pretending the value is legal.
    stubAnalysis({
      ...ENVELOPE,
      agentic: {
        ...ENVELOPE.agentic,
        root_cause: { ...ENVELOPE.agentic.root_cause, confidence: "certain" },
      },
    } as unknown as AgenticEnvelope);
    renderTab("r1");
    await screen.findByRole("heading", { name: /What to do/ });
    expect(screen.queryByText(/undefined/)).toBeNull();
    expect(screen.queryByText(/confidence/)).toBeNull();
  });

  it("surfaces a real failure as an inline error, with the server's own words", async () => {
    loginWithPerms(["ro_record"]);
    mswServer.use(
      http.get("/api/v1/record/r1/agentic", () =>
        HttpResponse.json(
          { error: { code: "forbidden", message: "record is out of your tenant" } },
          { status: 403 },
        ),
      ),
    );
    renderTab("r1");
    await waitFor(() => expect(screen.getByRole("alert")).toBeInTheDocument());
    expect(screen.getByRole("alert")).toHaveTextContent(/record is out of your tenant/i);
    expect(screen.queryByText("No analysis yet")).toBeNull();
  });
});
