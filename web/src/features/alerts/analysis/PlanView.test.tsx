import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import type { ReactNode } from "react";
import { TooltipProvider } from "@/shared/ui/Tooltip";
import { PlanView, type AgenticRemediationPlan } from "./PlanView";

function renderPlan(plan: AgenticRemediationPlan | undefined) {
  function Wrapper({ children }: { children: ReactNode }) {
    return <TooltipProvider>{children}</TooltipProvider>;
  }
  return render(<PlanView plan={plan} />, { wrapper: Wrapper });
}

describe("PlanView", () => {
  it("renders nothing for a plan with neither steps nor a rollback", () => {
    const { container } = renderPlan({ steps: [] });
    expect(container).toBeEmptyDOMElement();
  });

  it("titles the section with the question it answers", () => {
    renderPlan({ steps: [{ action: "Vacuum the journal", risk: "low" }] });
    expect(screen.getByRole("heading", { name: /What to do/ })).toBeInTheDocument();
  });

  it("marks an automatable plan in the heading, as a neutral chip", () => {
    renderPlan({ steps: [{ action: "Vacuum the journal", risk: "low" }], automatable: true });
    const heading = screen.getByRole("heading", { name: /What to do/ });
    expect(
      within(heading.parentElement as HTMLElement).getByText("Automatable"),
    ).toBeInTheDocument();
    expect(screen.queryByText("Manual")).toBeNull();
  });

  it("says 'Manual' — and why — for a plan that needs a human", () => {
    renderPlan({ steps: [{ action: "Page the DBA", risk: "high" }] });
    expect(screen.getByText("Manual")).toHaveAttribute(
      "title",
      "Needs a human: at least one step is risky or needs judgement",
    );
    // The old footer that restated the flag at the bottom of the plan is gone.
    expect(screen.queryByText(/Automatable:/)).toBeNull();
    expect(screen.queryByText(/Safe to run unattended/)).toBeNull();
  });

  it("marks only the steps worth a second look — low risk carries no chip", () => {
    renderPlan({
      steps: [
        { action: "Vacuum the journal", risk: "low" },
        { action: "Cap SystemMaxUse", risk: "medium" },
        { action: "Reboot the node", risk: "high" },
      ],
    });
    expect(screen.queryByText("Low risk")).toBeNull();
    expect(screen.getByText("Medium risk")).toBeInTheDocument();
    expect(screen.getByText("High risk")).toBeInTheDocument();
  });

  it("splits a plan whose steps say `when` into Now and Follow-up, keeping their numbers", () => {
    renderPlan({
      steps: [
        { action: "Vacuum the journal", risk: "low", when: "now" },
        { action: "Add a retention alert", risk: "low", when: "follow_up" },
        { action: "Restart journald", risk: "medium", when: "now" },
      ],
    });
    const now = screen.getByRole("list", { name: "Now" });
    const followUp = screen.getByRole("list", { name: "Follow-up" });
    expect(
      within(now)
        .getAllByRole("listitem")
        .map((li) => li.textContent),
    ).toEqual(["1Vacuum the journal", "3Restart journaldMedium risk"]);
    expect(
      within(followUp)
        .getAllByRole("listitem")
        .map((li) => li.textContent),
    ).toEqual(["2Add a retention alert"]);
  });

  it("keeps one list when no step says `when`", () => {
    renderPlan({
      steps: [
        { action: "Vacuum the journal", risk: "low" },
        { action: "Restart journald", risk: "low" },
      ],
    });
    expect(screen.queryByRole("list", { name: "Now" })).toBeNull();
    expect(screen.queryByRole("list", { name: "Follow-up" })).toBeNull();
    expect(screen.getAllByRole("listitem")).toHaveLength(2);
  });

  it("folds the rollback away until it is asked for", async () => {
    const user = userEvent.setup();
    renderPlan({
      steps: [{ action: "Vacuum the journal", risk: "low" }],
      rollback: [{ action: "Restore the previous journald.conf", risk: "low" }],
    });
    const toggle = screen.getByRole("button", { name: /Rollback/ });
    expect(toggle).toHaveAttribute("aria-expanded", "false");
    expect(screen.queryByText("Restore the previous journald.conf")).toBeNull();
    await user.click(toggle);
    expect(toggle).toHaveAttribute("aria-expanded", "true");
    expect(screen.getByText("Restore the previous journald.conf")).toBeInTheDocument();
  });
});
