import { render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { RequirePerm } from "./RequirePerm";
import { authStore } from "@/lib/auth/store";

function login(permissions: string[]) {
  const header = btoa(JSON.stringify({ alg: "HS256", typ: "JWT" }));
  const body = btoa(
    JSON.stringify({
      sub: "x",
      exp: Math.floor(Date.now() / 1000) + 3600,
      permissions,
    }),
  );
  authStore.getState().login(`${header}.${body}.sig`);
}

describe("RequirePerm", () => {
  beforeEach(() => {
    localStorage.clear();
    authStore.getState().logout();
  });
  afterEach(() => {
    localStorage.clear();
    authStore.getState().logout();
  });

  it("renders children when `any` permissions match", () => {
    login(["rw_rule"]);
    render(
      <RequirePerm any={["rw_rule"]}>
        <button>New rule</button>
      </RequirePerm>,
    );
    expect(screen.getByRole("button", { name: "New rule" })).toBeInTheDocument();
  });

  it("hides children when neither claims nor permissions match", () => {
    login(["ro_rule"]);
    render(
      <RequirePerm any={["rw_rule"]}>
        <button>New rule</button>
      </RequirePerm>,
    );
    expect(screen.queryByRole("button")).toBeNull();
  });

  it("renders fallback when denied", () => {
    login(["ro_rule"]);
    render(
      <RequirePerm any={["rw_rule"]} fallback={<span>read-only</span>}>
        <button>New rule</button>
      </RequirePerm>,
    );
    expect(screen.getByText("read-only")).toBeInTheDocument();
    expect(screen.queryByRole("button")).toBeNull();
  });

  it("renders nothing when not logged in", () => {
    render(
      <RequirePerm any={["rw_rule"]}>
        <button>New rule</button>
      </RequirePerm>,
    );
    expect(screen.queryByRole("button")).toBeNull();
  });

  it("supports `all` mode (require every permission)", () => {
    login(["rw_rule"]);
    const { rerender } = render(
      <RequirePerm all={["rw_rule", "rw_record"]}>
        <button>Mass action</button>
      </RequirePerm>,
    );
    expect(screen.queryByRole("button")).toBeNull();
    login(["rw_rule", "rw_record"]);
    rerender(
      <RequirePerm all={["rw_rule", "rw_record"]}>
        <button>Mass action</button>
      </RequirePerm>,
    );
    expect(screen.getByRole("button", { name: "Mass action" })).toBeInTheDocument();
  });
});
