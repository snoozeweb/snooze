import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import type { ReactNode } from "react";
import { describe, expect, it } from "vitest";
import { mswServer } from "@/tests/msw/server";
import { TooltipProvider } from "@/shared/ui/Tooltip";
import { CONSOLE_FALLBACK } from "@/features/config/types";
import { alertColumns } from "./columns";
import { OwnerFact } from "./Owner";
import { recordOwnership, sincePhrase } from "./ownership";
import type { Record_ } from "./types";

function wrap(node: ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <TooltipProvider delay={0}>{node}</TooltipProvider>
    </QueryClientProvider>,
  );
}

function ownerCell(row: Record_) {
  const column = alertColumns.find((c) => c.id === "owner");
  if (!column) throw new Error("no owner column");
  return wrap(<>{column.cell(row)}</>);
}

const now = () => Math.floor(Date.now() / 1000);

describe("owner column", () => {
  it("is in the offline default layout right after State, and never the flexible column", () => {
    const ids = CONSOLE_FALLBACK.columns;
    expect(ids.indexOf("owner")).toBe(ids.indexOf("state") + 1);
    const col = alertColumns.find((c) => c.id === "owner")!;
    expect(col.width).toBeDefined();
    // Rides the same tier as Hits so Message keeps its width budget.
    expect(col.hideBelow).toBe("lg");
    expect(col.cardRole).toBe("header");
  });

  it("shows the owner's face, named with since-when", async () => {
    mswServer.use(
      http.get("/api/v1/people", () =>
        HttpResponse.json({
          data: [{ name: "alice", method: "local", display_name: "Alice Martin" }],
        }),
      ),
    );
    const user = userEvent.setup();
    ownerCell({ uid: "r1", owner: "alice", owner_method: "local", owner_since: now() - 300 });
    const face = await screen.findByRole("img", { name: "Owner: Alice Martin, since 5m ago" });
    expect(face).toHaveAttribute("data-variant", "normal");
    await user.hover(face);
    expect((await screen.findAllByText("Owner since 5m ago")).length).toBeGreaterThan(0);
  });

  it("shows a ghost for the previous owner of an alert nobody owns now", () => {
    ownerCell({ uid: "r1", owner: "", previous_owner: "bob", previous_owner_method: "ldap" });
    expect(screen.getByRole("img", { name: "Previous owner: bob — unowned now" })).toHaveAttribute(
      "data-variant",
      "ghost",
    );
  });

  it("renders an em-dash when nobody owns or owned it", () => {
    ownerCell({ uid: "r1" });
    expect(screen.getByText("—")).toBeInTheDocument();
    expect(screen.queryByRole("img")).toBeNull();
  });
});

describe("recordOwnership", () => {
  it("never reports a ghost while someone owns the alert", () => {
    expect(recordOwnership({ owner: "alice", previous_owner: "bob" }).previous).toBe("");
  });
  it("treats an empty owner as unowned and drops its since", () => {
    expect(recordOwnership({ owner: "", owner_since: 0 })).toMatchObject({ owner: "", since: 0 });
  });
});

describe("sincePhrase", () => {
  it("reads naturally at both ends", () => {
    expect(sincePhrase(now())).toBe("since just now");
    expect(sincePhrase(now() - 7200)).toBe("since 2h ago");
    expect(sincePhrase(0)).toBe("");
  });
});

describe("OwnerFact", () => {
  it("names the owner and since-when", async () => {
    mswServer.use(
      http.get("/api/v1/people", () =>
        HttpResponse.json({
          data: [{ name: "alice", method: "local", display_name: "Alice Martin" }],
        }),
      ),
    );
    const { container } = wrap(
      <dl>
        <OwnerFact record={{ owner: "alice", owner_since: now() - 60 }} />
      </dl>,
    );
    await waitFor(() => expect(screen.getByText("Alice Martin")).toBeInTheDocument());
    expect(container.querySelector('[data-slot="owner"]')).toHaveTextContent(
      /Alice Martin · since/,
    );
  });

  it("names the previous owner of an unowned alert", () => {
    const { container } = wrap(
      <dl>
        <OwnerFact record={{ owner: "", previous_owner: "bob" }} />
      </dl>,
    );
    expect(container.querySelector('[data-slot="previous-owner"]')).toHaveTextContent(
      "Unowned · previously bob",
    );
  });

  it("renders nothing for an alert nobody ever owned", () => {
    const { container } = wrap(
      <dl>
        <OwnerFact record={{ uid: "r1" }} />
      </dl>,
    );
    expect(container.querySelector("dl")).toBeEmptyDOMElement();
  });
});
