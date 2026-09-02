import { describe, expect, it } from "vitest";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from "@tanstack/react-router";
import { AlertFlowChart } from "./AlertFlowChart";
import { TooltipProvider } from "@/shared/ui/Tooltip";
import type { Record_ } from "./types";

// AlertFlowChart renders TanStack <Link>s, which need a RouterProvider ancestor
// with the target routes registered, plus a TooltipProvider for chip hints
// (both are mounted app-wide in app/router.tsx). We stand up a minimal memory
// router whose home route hosts the chart — mirroring ActivityFeed.test.tsx.
function renderChart(row: Record_) {
  const root = createRootRoute({ component: () => <Outlet /> });
  const home = createRoute({
    getParentRoute: () => root,
    path: "/",
    component: () => (
      <TooltipProvider delay={0}>
        <AlertFlowChart row={row} />
      </TooltipProvider>
    ),
  });
  const stub = (path: string) =>
    createRoute({ getParentRoute: () => root, path, component: () => <div>{path}</div> });
  const tree = root.addChildren([
    home,
    stub("/web/rules"),
    stub("/web/snoozes"),
    stub("/web/notifications"),
  ]);
  /* eslint-disable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-argument */
  const router = createRouter({
    routeTree: tree,
    history: createMemoryHistory({ initialEntries: ["/"] }),
  } as any);
  /* eslint-enable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-argument */
  return render(
    <RouterProvider router={router as Parameters<typeof RouterProvider>[0]["router"]} />,
  );
}

// Pull the decoded query params out of a link's href.
function linkParams(name: string | RegExp): URLSearchParams {
  const href = screen.getByRole("link", { name }).getAttribute("href") ?? "";
  return new URL(href, "http://x").searchParams;
}

const base: Record_ = {
  uid: "u1",
  source: "syslog",
  rules: ["disk-warn", "env-tag"],
  aggregate: "Host and Message",
  hash: "abcdef0123456789",
};

describe("AlertFlowChart", () => {
  it("renders the input, rules and aggregate, then notifications and actions", () => {
    renderChart({
      ...base,
      notifications: ["oncall"],
      actions: [
        { name: "email", notification: "oncall", status: "success" },
        { name: "webhook", notification: "oncall", status: "error", error: "dial tcp timeout" },
      ],
    });
    expect(screen.getByText("syslog")).toBeInTheDocument();
    // Rules are now individual links, not a joined string.
    expect(screen.getByRole("link", { name: "disk-warn" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "env-tag" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "oncall" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /email/ })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /webhook/ })).toBeInTheDocument();
  });

  it("deep-links each rule to the Rules tab filtered by name", () => {
    renderChart(base);
    const disk = linkParams("disk-warn");
    expect(disk.get("tab")).toBe("rules");
    expect(disk.get("search")).toBe('name = "disk-warn"');
    expect(linkParams("env-tag").get("search")).toBe('name = "env-tag"');
    expect(screen.getByRole("link", { name: "disk-warn" }).getAttribute("href")).toContain(
      "/web/rules",
    );
  });

  it("deep-links the aggregate to the Aggregates tab filtered by name", () => {
    renderChart(base);
    const agg = linkParams("Host and Message");
    expect(agg.get("tab")).toBe("aggregates");
    expect(agg.get("aggSearch")).toBe('name = "Host and Message"');
    expect(screen.getByRole("link", { name: "Host and Message" }).getAttribute("href")).toContain(
      "/web/rules",
    );
  });

  it("renders the synthetic 'default' aggregate bucket as plain text, not a link", () => {
    renderChart({ ...base, aggregate: "default" });
    expect(screen.queryByRole("link", { name: "default" })).not.toBeInTheDocument();
    expect(screen.getByText("default")).toBeInTheDocument();
  });

  it("escapes quotes and backslashes in the deep-link query", () => {
    renderChart({ ...base, rules: ['weird"rule', "back\\slash"] });
    expect(linkParams('weird"rule').get("search")).toBe('name = "weird\\"rule"');
    expect(linkParams("back\\slash").get("search")).toBe('name = "back\\\\slash"');
  });

  it("is terminal at the snooze node when snoozed, and deep-links the snooze by name", () => {
    renderChart({
      ...base,
      snoozed: "maint-window",
      notifications: ["oncall"],
      actions: [{ name: "email", notification: "oncall", status: "success" }],
    });
    const snooze = screen.getByRole("link", { name: /maint-window/ });
    expect(snooze).toBeInTheDocument();
    expect(snooze.getAttribute("href")).toContain("/web/snoozes");
    expect(linkParams(/maint-window/).get("search")).toBe('name = "maint-window"');
    expect(screen.getByText("silenced — pipeline stopped")).toBeInTheDocument();
    // The stage still renders — it just says the run never got there. No
    // notification or action links, because neither ran.
    expect(screen.getByText("Notifications")).toBeInTheDocument();
    expect(screen.getByText("not reached — silenced upstream")).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "oncall" })).not.toBeInTheDocument();
    expect(screen.queryByRole("link", { name: /email/ })).not.toBeInTheDocument();
  });

  it("always renders the snooze stage, saying so when nothing matched", () => {
    renderChart({ ...base, notifications: ["oncall"] });
    expect(screen.getByText("Snooze")).toBeInTheDocument();
    expect(
      screen.getByText("No snooze matched — this one was meant to reach you"),
    ).toBeInTheDocument();
    // …and the pipeline carries on to the notifications it actually reached.
    expect(screen.getByRole("link", { name: "oncall" })).toBeInTheDocument();
    expect(screen.queryByText("not reached — silenced upstream")).not.toBeInTheDocument();
  });

  it("labels the aggregate hash as the group key and keeps the full value in the title", () => {
    renderChart(base);
    const label = screen.getByText("group key");
    const hash = label.parentElement!;
    expect(hash).toHaveTextContent("group key abcdef012345");
    // Truncated on screen, whole in the tooltip.
    expect(hash).not.toHaveTextContent("abcdef0123456789");
    expect(hash).toHaveAttribute("title", "abcdef0123456789");
  });

  it("deep-links a notification branch head to the Notifications tab by name", () => {
    renderChart({
      ...base,
      notifications: ["oncall"],
      actions: [{ name: "email", notification: "oncall", status: "success" }],
    });
    const n = linkParams("oncall");
    expect(n.get("tab")).toBe("notifications");
    expect(n.get("search")).toBe('name = "oncall"');
    expect(screen.getByRole("link", { name: "oncall" }).getAttribute("href")).toContain(
      "/web/notifications",
    );
  });

  it("deep-links an action chip to the Actions tab by name", () => {
    renderChart({
      ...base,
      notifications: ["oncall"],
      actions: [{ name: "email", notification: "oncall", status: "success" }],
    });
    const a = linkParams(/email/);
    expect(a.get("tab")).toBe("actions");
    expect(a.get("actionSearch")).toBe('name = "email"');
    expect(screen.getByRole("link", { name: /email/ }).getAttribute("href")).toContain(
      "/web/notifications",
    );
  });

  it("navigates on an error action and exposes the error via a hover tooltip", async () => {
    renderChart({
      ...base,
      notifications: ["oncall"],
      actions: [
        { name: "webhook", notification: "oncall", status: "error", error: "dial tcp timeout" },
      ],
    });
    // The error chip is now a navigation link (not a popover button).
    const chip = screen.getByRole("link", { name: /webhook/ });
    expect(linkParams(/webhook/).get("actionSearch")).toBe('name = "webhook"');
    // Its error text is reachable on hover, via the shared tooltip. Radix
    // renders the content plus a visually-hidden a11y copy, so match all.
    await userEvent.hover(chip);
    expect((await screen.findAllByText("dial tcp timeout")).length).toBeGreaterThan(0);
  });

  it("renders skipped/pending/sent action chips as links (no popover button)", () => {
    renderChart({
      ...base,
      notifications: ["oncall"],
      actions: [
        { name: "page", notification: "oncall", status: "skipped" },
        { name: "email", notification: "oncall", status: "pending" },
        { name: "webhook", notification: "oncall", status: "sent" },
      ],
    });
    expect(screen.getByRole("link", { name: /page/ })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /email/ })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /webhook/ })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /error details/i })).not.toBeInTheDocument();
  });

  it("collapses duplicate notification names into a single branch", () => {
    // Notification names are not unique across entries (entries are keyed by
    // uid), so a record can carry the same name twice. It must render one
    // branch, not two identical ones.
    renderChart({
      ...base,
      notifications: ["oncall", "oncall"],
      actions: [{ name: "email", notification: "oncall", status: "success" }],
    });
    expect(screen.getAllByRole("link", { name: "oncall" })).toHaveLength(1);
    expect(screen.getAllByRole("link", { name: /email/ })).toHaveLength(1);
  });

  it("shows the none placeholder for empty rules and actions on a minimal record", () => {
    renderChart({ uid: "u2", source: "prom" });
    // Empty rules and notifications each render the placeholder.
    expect(screen.getAllByText("none").length).toBeGreaterThanOrEqual(2);
    const node = (label: string) => screen.getByText(label).closest("div")!.parentElement!;
    expect(node("Rules")).toHaveTextContent("none");
    expect(node("Notifications")).toHaveTextContent("none");
    // Nothing to link on a minimal record.
    expect(screen.queryAllByRole("link")).toHaveLength(0);
  });

  it("forks into a branch per matched notification, each with its own actions", () => {
    renderChart({
      ...base,
      notifications: ["oncall", "slack-team"],
      actions: [
        { name: "email", notification: "oncall", status: "success" },
        { name: "pager", notification: "oncall", status: "error", error: "boom" },
        { name: "webhook", notification: "slack-team", status: "success" },
        { name: "sms", notification: "slack-team", status: "skipped" },
      ],
    });
    // Each notification head link → its branch card is head.parent.parent.
    const oncall = screen.getByRole("link", { name: "oncall" }).parentElement!.parentElement!;
    const slack = screen.getByRole("link", { name: "slack-team" }).parentElement!.parentElement!;
    // Actions appear under their own notification, not the other.
    expect(within(oncall).getByRole("link", { name: /email/ })).toBeInTheDocument();
    expect(within(oncall).getByRole("link", { name: /pager/ })).toBeInTheDocument();
    expect(within(slack).getByRole("link", { name: /webhook/ })).toBeInTheDocument();
    expect(within(slack).getByRole("link", { name: /sms/ })).toBeInTheDocument();
    expect(within(slack).queryByRole("link", { name: /email/ })).not.toBeInTheDocument();
  });
});
