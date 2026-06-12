import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import type { UseMutationResult, UseQueryResult } from "@tanstack/react-query";
import { TooltipProvider } from "@/shared/ui/Tooltip";
import { ToastProvider, Toaster } from "@/shared/ui/Toast";
import { Input } from "@/shared/ui/Input";
import { ApiError } from "@/lib/api/client";
import { toastStore } from "@/shared/ui/toast/useToast";
import { EditorAbort, EditorDrawer, useFieldInvalid, type EditorBodyProps } from "./EditorDrawer";

// jsdom polyfill — Radix Drawer uses ResizeObserver internally.
beforeAll(() => {
  if (typeof window !== "undefined" && !window.ResizeObserver) {
    window.ResizeObserver = class ResizeObserver {
      observe() {}
      unobserve() {}
      disconnect() {}
    };
  }
});

// toastStore is a module singleton; clear it between tests so a success/error
// toast from one case cannot leak into the next case's assertions.
beforeEach(() => {
  toastStore.clear();
});

function wrap() {
  return ({ children }: { children: ReactNode }) => (
    <TooltipProvider delay={0}>
      <ToastProvider>
        {children}
        <Toaster />
      </ToastProvider>
    </TooltipProvider>
  );
}

type Rec = { uid?: string; name: string };
type Form = { name: string };

// Minimal fakes for the TanStack Query hook results the frame consumes. We
// only populate the fields the frame actually reads (data / isPending /
// mutateAsync) and cast the rest — this keeps the frame test free of a real
// QueryClient and isolates it to the chrome + lifecycle it owns.
function fakeGet(over: Partial<UseQueryResult<Rec, ApiError>> = {}) {
  return { data: undefined, isPending: false, ...over } as unknown as UseQueryResult<Rec, ApiError>;
}
function fakeCreate(mutateAsync: (b: Partial<Rec>) => Promise<Rec>) {
  return { mutateAsync } as unknown as UseMutationResult<Rec, ApiError, Partial<Rec>>;
}
function fakeUpdate(mutateAsync: (v: { uid: string; body: Partial<Rec> }) => Promise<Rec>) {
  return { mutateAsync } as unknown as UseMutationResult<
    Rec,
    ApiError,
    { uid: string; body: Partial<Rec> }
  >;
}

type HarnessProps = {
  uid?: string;
  onClose?: () => void;
  get?: UseQueryResult<Rec, ApiError>;
  createImpl?: (b: Partial<Rec>) => Promise<Rec>;
  updateImpl?: (v: { uid: string; body: Partial<Rec> }) => Promise<Rec>;
  onCreated?: (r: Rec) => boolean | void | Promise<boolean | void>;
  abortSubmit?: boolean;
};

function Harness({
  uid,
  onClose = () => {},
  get = fakeGet(),
  createImpl = (b) => Promise.resolve({ uid: "new", name: b.name ?? "" }),
  updateImpl = (v) => Promise.resolve({ uid: v.uid, name: v.body.name ?? "" }),
  onCreated,
  abortSubmit,
}: HarnessProps) {
  return (
    <EditorDrawer<Form, Rec>
      uid={uid}
      onClose={onClose}
      get={get}
      create={fakeCreate(createImpl)}
      update={fakeUpdate(updateImpl)}
      emptyForm={{ name: "" }}
      recordToForm={(r) => ({ name: r.name ?? "" })}
      formToBody={(form) => {
        if (abortSubmit) throw new EditorAbort();
        return { name: form.name };
      }}
      title={(c) => (c ? "New thing" : "Edit thing")}
      successMessage={{ create: "Thing created", update: "Thing saved" }}
      formId="thing-form"
      {...(onCreated ? { onCreated } : {})}
    >
      {(body) => <ThingFields {...body} />}
    </EditorDrawer>
  );
}

function ThingFields({ register, control }: EditorBodyProps<Form>) {
  const invalid = useFieldInvalid(control, "name");
  return (
    <div>
      <label htmlFor="thing-name">Name</label>
      <Input id="thing-name" {...register("name")} invalid={invalid} />
    </div>
  );
}

describe("EditorDrawer", () => {
  it("shows the create title and Create button in create mode", () => {
    const Wrapper = wrap();
    render(
      <Wrapper>
        <Harness />
      </Wrapper>,
    );
    expect(screen.getByText("New thing")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /create/i })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /^save$/i })).not.toBeInTheDocument();
  });

  it("posts the mapped body, toasts success, and closes on Create", async () => {
    const bodies: Partial<Rec>[] = [];
    const onClose = vi.fn();
    const Wrapper = wrap();
    const user = userEvent.setup();
    render(
      <Wrapper>
        <Harness
          onClose={onClose}
          createImpl={(b) => {
            bodies.push(b);
            return Promise.resolve({ uid: "new", name: b.name ?? "" });
          }}
        />
      </Wrapper>,
    );
    await user.type(screen.getByLabelText(/^name$/i), "alpha");
    await user.click(screen.getByRole("button", { name: /create/i }));
    await waitFor(() => expect(bodies).toHaveLength(1));
    expect(bodies[0]?.name).toBe("alpha");
    expect(await screen.findByText("Thing created")).toBeInTheDocument();
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("shows the edit title/Save button and resets the form from loaded data", async () => {
    const Wrapper = wrap();
    render(
      <Wrapper>
        <Harness uid="x1" get={fakeGet({ data: { uid: "x1", name: "loaded" } })} />
      </Wrapper>,
    );
    expect(screen.getByText("Edit thing")).toBeInTheDocument();
    await waitFor(() =>
      expect(screen.getByLabelText<HTMLInputElement>(/^name$/i).value).toBe("loaded"),
    );
    expect(screen.getByRole("button", { name: /^save$/i })).toBeInTheDocument();
  });

  it("renders the load spinner while an edit-mode get is pending", () => {
    const Wrapper = wrap();
    render(
      <Wrapper>
        <Harness uid="x1" get={fakeGet({ isPending: true })} />
      </Wrapper>,
    );
    // The load Spinner (role=status, aria-label "Loading") is shown and the
    // form/field is not yet rendered.
    expect(screen.getByRole("status", { name: /loading/i })).toBeInTheDocument();
    expect(screen.queryByLabelText(/^name$/i)).not.toBeInTheDocument();
  });

  it("toasts the ApiError detail and does not close when the mutation fails", async () => {
    const onClose = vi.fn();
    const Wrapper = wrap();
    const user = userEvent.setup();
    render(
      <Wrapper>
        <Harness
          onClose={onClose}
          createImpl={() => Promise.reject(new ApiError(409, "conflict", "name already taken"))}
        />
      </Wrapper>,
    );
    await user.type(screen.getByLabelText(/^name$/i), "dup");
    await user.click(screen.getByRole("button", { name: /create/i }));
    expect(await screen.findByText("name already taken")).toBeInTheDocument();
    expect(onClose).not.toHaveBeenCalled();
  });

  it("EditorAbort from formToBody cancels the submit silently (no toast, no close)", async () => {
    const onClose = vi.fn();
    const created = vi.fn((b: Partial<Rec>) => Promise.resolve({ uid: "new", name: b.name ?? "" }));
    const Wrapper = wrap();
    const user = userEvent.setup();
    render(
      <Wrapper>
        <Harness onClose={onClose} abortSubmit createImpl={created} />
      </Wrapper>,
    );
    await user.type(screen.getByLabelText(/^name$/i), "alpha");
    await user.click(screen.getByRole("button", { name: /create/i }));
    // Give any async path a tick; nothing should have fired.
    await new Promise((r) => setTimeout(r, 20));
    expect(created).not.toHaveBeenCalled();
    expect(onClose).not.toHaveBeenCalled();
    expect(screen.queryByText("Thing created")).not.toBeInTheDocument();
    // The submit button re-enabled (submitting flipped back to false).
    expect(screen.getByRole("button", { name: /create/i })).not.toBeDisabled();
  });

  it("onCreated returning false suppresses the frame's auto-close", async () => {
    const onClose = vi.fn();
    const onCreated = vi.fn(() => false);
    const Wrapper = wrap();
    const user = userEvent.setup();
    render(
      <Wrapper>
        <Harness onClose={onClose} onCreated={onCreated} />
      </Wrapper>,
    );
    await user.type(screen.getByLabelText(/^name$/i), "alpha");
    await user.click(screen.getByRole("button", { name: /create/i }));
    await waitFor(() => expect(onCreated).toHaveBeenCalledTimes(1));
    expect(onClose).not.toHaveBeenCalled();
  });

  it("dirty-close guard: a pristine close calls onClose directly", async () => {
    const onClose = vi.fn();
    const Wrapper = wrap();
    const user = userEvent.setup();
    render(
      <Wrapper>
        <Harness onClose={onClose} />
      </Wrapper>,
    );
    await user.click(screen.getByRole("button", { name: /cancel/i }));
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(screen.queryByText("Discard changes?")).not.toBeInTheDocument();
  });

  it("dirty-close guard: a dirty close opens the Discard dialog; Keep editing aborts, Discard closes", async () => {
    const onClose = vi.fn();
    const Wrapper = wrap();
    const user = userEvent.setup();
    render(
      <Wrapper>
        <Harness onClose={onClose} />
      </Wrapper>,
    );
    // Make the form dirty.
    await user.type(screen.getByLabelText(/^name$/i), "edit");
    await user.click(screen.getByRole("button", { name: /cancel/i }));
    // The in-DOM confirm appears (no window.confirm).
    expect(await screen.findByText("Discard changes?")).toBeInTheDocument();
    expect(onClose).not.toHaveBeenCalled();
    // Keep editing dismisses without closing.
    await user.click(screen.getByRole("button", { name: /keep editing/i }));
    await waitFor(() => expect(screen.queryByText("Discard changes?")).not.toBeInTheDocument());
    expect(onClose).not.toHaveBeenCalled();
    // Re-open and Discard this time.
    await user.click(screen.getByRole("button", { name: /cancel/i }));
    await user.click(await screen.findByRole("button", { name: /^discard$/i }));
    expect(onClose).toHaveBeenCalledTimes(1);
  });
});
