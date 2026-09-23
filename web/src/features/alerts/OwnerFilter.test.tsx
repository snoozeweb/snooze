import { useState } from "react";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { afterEach, describe, expect, it, vi } from "vitest";
import { mswServer } from "@/tests/msw/server";
import { authStore } from "@/lib/auth/store";
import { TooltipProvider } from "@/shared/ui/Tooltip";
import { OwnerFilter, OWNER_CHIPS_INLINE } from "./OwnerFilter";

function loginAs(sub: string) {
  const header = btoa(JSON.stringify({ alg: "HS256", typ: "JWT" }));
  const body = btoa(
    JSON.stringify({
      sub,
      method: "local",
      exp: Math.floor(Date.now() / 1000) + 3600,
      permissions: ["ro_record"],
    }),
  );
  authStore.getState().login(`${header}.${body}.sig`);
}

function counts(data: Array<{ owner: string; count: number }>, unowned = 0) {
  return HttpResponse.json({
    data,
    unowned,
    total: data.reduce((n, c) => n + c.count, unowned),
  });
}

function harness(initial: string[] = [], countsQ?: string) {
  const onChange = vi.fn();
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  function Wrapper() {
    const [value, setValue] = useState<string[]>(initial);
    return (
      <OwnerFilter
        value={value}
        onChange={(next) => {
          setValue(next);
          onChange(next);
        }}
        countsQ={countsQ}
      />
    );
  }
  render(
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <Wrapper />
      </TooltipProvider>
    </QueryClientProvider>,
  );
  return { onChange };
}

const group = () => screen.getByRole("group", { name: "Filter by owner" });

describe("OwnerFilter", () => {
  afterEach(() => authStore.getState().logout({ revoke: false }));

  it("puts me first, greyed at zero, then owners by count, then Unowned", async () => {
    loginAs("me");
    mswServer.use(
      http.get("/api/v1/record/owners", () =>
        counts(
          [
            { owner: "bob", count: 5 },
            { owner: "carol", count: 2 },
          ],
          7,
        ),
      ),
      http.get("/api/v1/people", () =>
        HttpResponse.json({
          data: [
            { name: "bob", method: "local", display_name: "Bob Stone" },
            { name: "carol", method: "local" },
            { name: "me", method: "local", display_name: "Me Myself" },
          ],
        }),
      ),
    );
    harness();
    await waitFor(() =>
      expect(within(group()).getByRole("button", { name: "Bob Stone, 5 alerts" })).toBeVisible(),
    );
    const names = within(group())
      .getAllByRole("button")
      .map((b) => b.getAttribute("aria-label"));
    expect(names).toEqual([
      "Me Myself (you), 0 alerts",
      "Bob Stone, 5 alerts",
      "carol, 2 alerts",
      "Unowned, 7 alerts",
    ]);
    const mine = within(group()).getByRole("button", { name: /\(you\)/ });
    expect(mine).toHaveAttribute("data-empty", "true");
    expect(mine).toHaveAttribute("aria-pressed", "false");
  });

  it("counts over the condition it is given, not over its own selection", async () => {
    const seen: Array<string | null> = [];
    mswServer.use(
      http.get("/api/v1/record/owners", ({ request }) => {
        seen.push(new URL(request.url).searchParams.get("q"));
        return counts([{ owner: "bob", count: 1 }]);
      }),
    );
    harness(["bob"], "abc123");
    await waitFor(() => expect(seen).toContain("abc123"));
    expect(seen.every((q) => q === "abc123")).toBe(true);
  });

  it("toggles owners as an OR selection and Unowned as its own token", async () => {
    mswServer.use(
      http.get("/api/v1/record/owners", () =>
        counts(
          [
            { owner: "bob", count: 5 },
            { owner: "carol", count: 2 },
          ],
          1,
        ),
      ),
    );
    const user = userEvent.setup();
    const { onChange } = harness();
    await user.click(await screen.findByRole("button", { name: "bob, 5 alerts" }));
    expect(onChange).toHaveBeenLastCalledWith(["bob"]);
    await user.click(screen.getByRole("button", { name: "carol, 2 alerts" }));
    expect(onChange).toHaveBeenLastCalledWith(["bob", "carol"]);
    await user.click(screen.getByRole("button", { name: "Unowned, 1 alert" }));
    expect(onChange).toHaveBeenLastCalledWith(["bob", "carol", "~none"]);
    expect(screen.getByRole("button", { name: "bob, 5 alerts" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    await user.click(screen.getByRole("button", { name: "bob, 5 alerts" }));
    expect(onChange).toHaveBeenLastCalledWith(["carol", "~none"]);
  });

  it("keeps a selected owner with nothing in this view on the strip, so it can be deselected", async () => {
    mswServer.use(http.get("/api/v1/record/owners", () => counts([{ owner: "bob", count: 3 }])));
    harness(["dave"]);
    const dave = await screen.findByRole("button", { name: "dave, 0 alerts" });
    expect(dave).toHaveAttribute("aria-pressed", "true");
  });

  it("folds the long tail behind +N, with a search, and toggles from there", async () => {
    const many = Array.from({ length: OWNER_CHIPS_INLINE + 3 }, (_, i) => ({
      owner: `user${i}`,
      count: 20 - i,
    }));
    mswServer.use(http.get("/api/v1/record/owners", () => counts(many)));
    const user = userEvent.setup();
    const { onChange } = harness();
    const more = await screen.findByRole("button", { name: "3 more owners" });
    // No signed-in user here, so all six inline chips are other people.
    expect(within(group()).getAllByRole("button", { name: /^user\d/ })).toHaveLength(
      OWNER_CHIPS_INLINE,
    );
    await user.click(more);
    const search = await screen.findByRole("textbox", { name: "Search owners" });
    await user.type(search, "user8");
    const list = screen.getByRole("group", { name: "More owners" });
    expect(within(list).getAllByRole("button")).toHaveLength(1);
    await user.click(within(list).getByRole("button", { name: /user8/ }));
    expect(onChange).toHaveBeenLastCalledWith(["user8"]);
    // Selected, so it is promoted inline and the fold shrinks.
    await waitFor(() =>
      expect(within(group()).getByRole("button", { name: "user8, 12 alerts" })).toHaveAttribute(
        "aria-pressed",
        "true",
      ),
    );
    expect(within(group()).getByRole("button", { name: "2 more owners" })).toBeInTheDocument();
  });
});
