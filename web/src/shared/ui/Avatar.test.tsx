import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import type { ReactNode } from "react";
import { describe, expect, it } from "vitest";
import { mswServer } from "@/tests/msw/server";
import { Avatar } from "./Avatar";
import { AVATAR_TONES, avatarTone, initialsOf } from "./avatarUtils";
import { TooltipProvider } from "./Tooltip";

// 1×1 transparent PNG — just something `<img src>` accepts.
const PNG =
  "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNkYAAAAAYAAjCB0C8AAAAASUVORK5CYII=";

function wrap(children: ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <TooltipProvider delay={0}>{children}</TooltipProvider>
    </QueryClientProvider>,
  );
}

describe("initialsOf", () => {
  it("takes the first letter of the first two words", () => {
    expect(initialsOf("Alice Martin")).toBe("AM");
    expect(initialsOf("alice.martin")).toBe("AM");
    expect(initialsOf("jean-luc picard riker")).toBe("JL");
  });
  it("takes one letter from a one-word name", () => {
    expect(initialsOf("alice")).toBe("A");
  });
  it("keeps an accented first letter and never returns nothing", () => {
    expect(initialsOf("émile zola")).toBe("ÉZ");
    expect(initialsOf("")).toBe("?");
  });
});

describe("avatarTone", () => {
  it("is deterministic and always a valid slot", () => {
    for (const name of ["alice", "bob", "carol", "d", "", "李雷"]) {
      const t = avatarTone(name);
      expect(avatarTone(name)).toBe(t);
      expect(t).toBeGreaterThanOrEqual(1);
      expect(t).toBeLessThanOrEqual(AVATAR_TONES);
    }
  });
  it("spreads a handful of logins over more than one slot", () => {
    const tones = new Set(["alice", "bob", "carol", "dave", "erin", "frank"].map(avatarTone));
    expect(tones.size).toBeGreaterThan(2);
  });
});

describe("Avatar", () => {
  it("renders initials from the directory's display name, named for assistive tech", async () => {
    let avatarRequests = 0;
    mswServer.use(
      http.get("/api/v1/people", () =>
        HttpResponse.json({
          data: [{ name: "alice", method: "local", display_name: "Alice Martin" }],
        }),
      ),
      http.get("/api/v1/avatar/:method/:name", () => {
        avatarRequests++;
        return HttpResponse.json({ error: { code: "not_found" } }, { status: 404 });
      }),
    );
    wrap(<Avatar name="alice" />);
    const img = await screen.findByRole("img", { name: "Alice Martin" });
    await waitFor(() => expect(img).toHaveTextContent("AM"));
    expect(img).toHaveAttribute("data-tone", String(avatarTone("alice")));
    // No avatar_version in the directory → no picture request at all.
    expect(avatarRequests).toBe(0);
  });

  it("falls back to the login when the person is not in the directory", () => {
    wrap(<Avatar name="bob" />);
    expect(screen.getByRole("img", { name: "bob" })).toHaveTextContent("B");
  });

  it("fetches and paints the picture when the directory has a version for it", async () => {
    const seen: string[] = [];
    mswServer.use(
      http.get("/api/v1/people", () =>
        HttpResponse.json({ data: [{ name: "alice", method: "ldap", avatar_version: "abc123" }] }),
      ),
      http.get("/api/v1/avatar/:method/:name", ({ params }) => {
        seen.push(`${String(params.method)}/${String(params.name)}`);
        return HttpResponse.json({ name: "alice", method: "ldap", version: "abc123", data: PNG });
      }),
    );
    const { container } = wrap(
      <>
        <Avatar name="alice" />
        <Avatar name="alice" size="sm" />
      </>,
    );
    await waitFor(() => expect(container.querySelectorAll("img")).toHaveLength(2));
    expect(container.querySelector("img")).toHaveAttribute("src", PNG);
    // Two faces, one cache entry, one request — addressed by the directory's method.
    expect(seen).toEqual(["ldap/alice"]);
  });

  it("marks a previous owner as a ghost and a system actor as a bot", () => {
    wrap(
      <>
        <Avatar name="carol" variant="ghost" label="Previous owner: carol" />
        <Avatar name="" variant="bot" />
      </>,
    );
    expect(screen.getByRole("img", { name: "Previous owner: carol" })).toHaveAttribute(
      "data-variant",
      "ghost",
    );
    const bot = screen.getByRole("img", { name: "System" });
    expect(bot).toHaveAttribute("data-variant", "bot");
    expect(bot).not.toHaveAttribute("data-tone");
  });

  it("stays out of the accessibility tree when decorative", () => {
    const { container } = wrap(<Avatar name="dave" decorative />);
    expect(screen.queryByRole("img")).toBeNull();
    expect(container.firstElementChild).toHaveAttribute("aria-hidden", "true");
  });

  it("shows its tooltip on hover", async () => {
    const user = userEvent.setup();
    wrap(<Avatar name="erin" tooltip="Erin · owner since 5m" />);
    await user.hover(screen.getByRole("img", { name: "erin" }));
    expect((await screen.findAllByText("Erin · owner since 5m")).length).toBeGreaterThan(0);
  });

  it("prefers an explicit src (the profile preview) over the stored picture", () => {
    const { container } = wrap(<Avatar name="frank" src={PNG} size="lg" />);
    expect(container.querySelector("img")).toHaveAttribute("src", PNG);
  });
});
