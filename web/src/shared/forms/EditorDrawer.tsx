import { useEffect, useState, type ReactNode } from "react";
import {
  useForm,
  useFormState,
  useWatch,
  type Control,
  type FieldValues,
  type Path,
  type UseFormRegister,
  type UseFormSetValue,
  type UseFormWatch,
} from "react-hook-form";
import type { UseMutationResult, UseQueryResult } from "@tanstack/react-query";
import { Button } from "@/shared/ui/Button";
import { Dialog, DialogBody, DialogContent, DialogFooter, DialogTitle } from "@/shared/ui/Dialog";
import { Drawer, DrawerBody, DrawerContent, DrawerFooter, DrawerTitle } from "@/shared/ui/Drawer";
import { Spinner } from "@/shared/ui/Spinner";
import { toast } from "@/shared/ui/toast/useToast";
import { ApiError } from "@/lib/api/client";

/**
 * EditorDrawer — a compound frame that owns the scaffolding every Snooze
 * record editor shared verbatim: the Drawer chrome (title / body / footer),
 * the load spinner while {@link useGet} resolves, the reset-on-data effect,
 * the submitting flag, the create-vs-update branch, the ApiError-aware
 * toasts, the Cancel button, the Create/Save submit button, and a
 * dirty-close guard (an in-DOM "Discard changes?" dialog — never
 * `window.confirm`, which Playwright auto-dismisses).
 *
 * Editors keep ONLY their field layout and their wire mapping. They render
 * fields via the `children` render prop and describe the wire shape via
 * {@link EditorDrawerProps.recordToForm} / {@link EditorDrawerProps.formToBody}.
 *
 * React Hook Form remains the single source of truth for form state — the
 * frame creates the `useForm` instance internally and threads `control` /
 * `register` / `setValue` / `watch` to the render prop. The frame's value is
 * chrome + lifecycle, not state indirection.
 *
 * ## Render-prop payload ({@link EditorBodyProps})
 * - `control`   — RHF Control, for scoped `useWatch`/`useFormState` subcomponents.
 * - `register`  — RHF register, for uncontrolled inputs.
 * - `setValue`  — RHF setValue, for controlled widgets (Switch, MultiCombobox…).
 * - `watch`     — RHF watch, for inline reactive reads.
 * - `isCreate`  — true when no uid was supplied (create mode).
 * - `formId`    — the `<form>` id; the footer submit button targets it.
 *
 * ## Escape hatches (designed for the harder phase-2 editors)
 * - `title` may be a `(isCreate) => string` for create/edit-specific titles
 *   that also fold in extra context (RuleEditor's insertion position).
 * - `titleToolbar` renders beside the title (Snooze/Rule's Enabled switch).
 * - `footerStart` renders at the footer's left edge, flexed to fill the gap
 *   before Cancel/Save (Rule/Snooze's Diff section).
 *   Both `titleToolbar` and `footerStart` accept either a static node or a
 *   render function `(body) => ReactNode` receiving the same {@link
 *   EditorBodyProps} payload as `children` — so a reactive Enabled switch or a
 *   Diff section can take `control` and run its OWN scoped `useWatch` without
 *   re-rendering the whole drawer. The frame renders each through a stable
 *   component frame, so the function form may call hooks directly.
 * - `onCreated(result)` overrides the post-create behaviour. Return `false`
 *   (or a Promise resolving to `false`) to SUPPRESS the frame's auto-close —
 *   e.g. TenantEditor reveals a credential dialog instead of closing. Return
 *   anything else (or nothing) to keep the default close-on-success.
 * - `formToBody(form, isCreate)` may THROW {@link EditorAbort} to abort the
 *   submit silently after the editor has surfaced its own validation error
 *   (WidgetEditor's JSON / int parsing). The frame treats EditorAbort as a
 *   no-op: no toast, no close, submitting flips back to false.
 * - To render extra dialogs (Tenant's delete-confirm / credential reveal),
 *   wrap `<EditorDrawer>` in a fragment and place sibling dialogs beside it —
 *   the frame renders its own Discard dialog as a sibling of the Drawer, so
 *   editors may freely add more.
 *
 * @typeParam Form - the react-hook-form field-values shape.
 * @typeParam Record - the resource record type returned by useGet/useCreate.
 * @typeParam CreateBody - the POST body type accepted by useCreate.
 * @typeParam UpdateBody - the PATCH body type accepted by useUpdate.
 */

/**
 * Throw from {@link EditorDrawerProps.formToBody} to abort a submit after the
 * editor has already shown its own inline validation error. The frame
 * swallows it: no toast, no onClose, the submit button re-enables.
 */
export class EditorAbort extends Error {
  constructor() {
    super("EditorAbort");
    this.name = "EditorAbort";
  }
}

/**
 * Reactive "required field is empty after a submit attempt" flag, scoped to
 * its own RHF subscriptions so it re-renders only its caller — not the whole
 * drawer — on keystroke or submit. Ports the per-field invalid pattern the
 * hand-written editors expressed as `formState.isSubmitted && !watch(f).trim()`.
 *
 * Call it from inside the `children` render prop with the same `control` it
 * receives.
 */
export function useFieldInvalid<Form extends FieldValues>(
  control: Control<Form>,
  field: Path<Form>,
): boolean {
  // Only meaningful for string fields (the editors call it for name/dict/key).
  const value = useWatch({ control, name: field }) as unknown;
  const { isSubmitted } = useFormState({ control });
  const text = typeof value === "string" ? value : "";
  return isSubmitted && !text.trim();
}

/** The payload handed to the `children` render prop. */
export type EditorBodyProps<Form extends FieldValues> = {
  control: Control<Form>;
  register: UseFormRegister<Form>;
  setValue: UseFormSetValue<Form>;
  watch: UseFormWatch<Form>;
  isCreate: boolean;
  /** The `<form>` element id; the footer's submit button targets it. */
  formId: string;
};

export type EditorDrawerProps<
  Form extends FieldValues,
  Record,
  CreateBody = Partial<Record>,
  UpdateBody = Partial<Record>,
  /** The create mutation's RESULT type — defaults to the record type, but
   *  some resources return a richer envelope (e.g. tenant create returns a
   *  one-time admin credential alongside the write result). `onCreated`
   *  receives this. */
  CreateResult = Record,
> = {
  /** undefined / "" => create mode; otherwise edit the record with this uid. */
  uid: string | undefined;
  onClose: () => void;

  /** Resource hook RESULTS (call the defineResource hooks in the editor and
   *  pass them in — keeps the frame trivially testable without a real query
   *  client). */
  get: UseQueryResult<Record, ApiError>;
  create: UseMutationResult<CreateResult, ApiError, CreateBody>;
  update: UseMutationResult<Record, ApiError, { uid: string; body: UpdateBody }>;

  /** RHF default values for create mode and the reset baseline. */
  emptyForm: Form;
  /** Map a loaded record into form values (edit mode reset). */
  recordToForm: (record: Record) => Form;
  /** Map form values into the wire body. `isCreate` lets the body differ
   *  between POST and PATCH (e.g. UserEditor sends `method` only on create,
   *  `password` only when set). May throw {@link EditorAbort} to cancel the
   *  submit after surfacing an inline error. */
  formToBody: (form: Form, isCreate: boolean) => CreateBody | UpdateBody;

  /** Drawer title. A string is used as-is; a function gets `isCreate`. */
  title: string | ((isCreate: boolean) => ReactNode);
  /** Rendered beside the title (e.g. an Enabled switch). A function form
   *  receives the body payload so it can run scoped `useWatch` off `control`. */
  titleToolbar?: ReactNode | ((body: EditorBodyProps<Form>) => ReactNode) | undefined;
  /** Rendered at the footer's left edge, flexed to fill (e.g. a Diff). A
   *  function form receives the body payload for scoped subscriptions. */
  footerStart?: ReactNode | ((body: EditorBodyProps<Form>) => ReactNode) | undefined;

  /** Toast text on success. Strings or an {create, update} pair. */
  successMessage: string | { create: string; update: string };

  /** Stable form element id (also used to scope inputs). */
  formId: string;
  /** className for the inner `<form>` (usually the editor's `styles.stack`). */
  formClassName?: string | undefined;

  /** Override post-create behaviour. Return `false` to suppress auto-close
   *  (the editor will close itself later, e.g. after a credential dialog). */
  onCreated?: ((result: CreateResult) => boolean | void | Promise<boolean | void>) | undefined;

  /** Field layout, as a render prop receiving the RHF handles. The frame
   *  renders this as a component element (not an inline call), so it is safe
   *  to call hooks — `useWatch`, `useFieldInvalid`, `useFormState` — directly
   *  inside it. */
  children: (body: EditorBodyProps<Form>) => ReactNode;
};

const SPINNER_WRAP_STYLE: React.CSSProperties = {
  display: "flex",
  justifyContent: "center",
  padding: "var(--space-5)",
};

const FOOTER_START_STYLE: React.CSSProperties = { flex: 1 };

export function EditorDrawer<
  Form extends FieldValues,
  Record,
  CreateBody = Partial<Record>,
  UpdateBody = Partial<Record>,
  CreateResult = Record,
>(props: EditorDrawerProps<Form, Record, CreateBody, UpdateBody, CreateResult>) {
  const {
    uid,
    onClose,
    get,
    create,
    update,
    emptyForm,
    recordToForm,
    formToBody,
    title,
    titleToolbar,
    footerStart,
    successMessage,
    formId,
    formClassName,
    onCreated,
    children,
  } = props;

  const isCreate = uid === undefined || uid === "";

  const form = useForm<Form>({ defaultValues: emptyForm as never });
  const { register, handleSubmit, reset, control, setValue, watch } = form;

  // Subscribe to the dirty flag so a user-initiated close with unsaved edits
  // raises an in-DOM confirm step instead of silently discarding. isDirty
  // toggles once (false→true on first edit) so this costs at most one extra
  // drawer re-render per session — not one per keystroke.
  const { isDirty } = useFormState({ control });
  const [confirmDiscardOpen, setConfirmDiscardOpen] = useState(false);

  // The save path calls onClose() directly (see onSubmit) and never routes
  // through here, so a successful Create/Save closes without the guard. Only
  // the user-initiated close affordances (Escape / X / Cancel / overlay) hit
  // requestClose, and only a dirty form raises the confirm.
  function requestClose() {
    if (isDirty) {
      setConfirmDiscardOpen(true);
      return;
    }
    onClose();
  }

  useEffect(() => {
    if (isCreate) {
      reset(emptyForm as never);
      return;
    }
    if (get.data) {
      reset(recordToForm(get.data) as never);
    }
    // emptyForm/recordToForm are stable per editor render; depending on
    // get.data + isCreate matches every hand-written editor's effect deps.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [get.data, isCreate, reset]);

  const [submitting, setSubmitting] = useState(false);

  async function onSubmit(values: Form) {
    setSubmitting(true);
    try {
      let body: CreateBody | UpdateBody;
      try {
        body = formToBody(values, isCreate);
      } catch (e) {
        if (e instanceof EditorAbort) {
          // The editor already surfaced an inline error; abort quietly.
          setSubmitting(false);
          return;
        }
        throw e;
      }

      if (isCreate || uid === undefined || uid === "") {
        const result = await create.mutateAsync(body as CreateBody);
        toast.success(typeof successMessage === "string" ? successMessage : successMessage.create);
        if (onCreated) {
          const keepOpen = (await onCreated(result)) === false;
          if (keepOpen) return;
        }
      } else {
        await update.mutateAsync({ uid, body: body as UpdateBody });
        toast.success(typeof successMessage === "string" ? successMessage : successMessage.update);
      }
      onClose();
    } catch (e) {
      toast.error(e instanceof ApiError ? e.detail : "Save failed");
    } finally {
      setSubmitting(false);
    }
  }

  const titleNode = typeof title === "function" ? title(isCreate) : title;

  const bodyProps: EditorBodyProps<Form> = {
    control,
    register,
    setValue,
    watch,
    isCreate,
    formId,
  };

  // titleToolbar / footerStart may be a static node or a render function. The
  // function form is invoked inside a stable component frame (EditorBody) so
  // it may call hooks (scoped useWatch off `control`) directly, and so a fresh
  // closure each render does not remount the subtree.
  const toolbarNode =
    typeof titleToolbar === "function" ? (
      <EditorBody render={titleToolbar} body={bodyProps} />
    ) : (
      titleToolbar
    );
  const footerStartNode =
    typeof footerStart === "function" ? (
      <EditorBody render={footerStart} body={bodyProps} />
    ) : (
      footerStart
    );

  return (
    <Drawer
      open
      onOpenChange={(o) => {
        if (!o) requestClose();
      }}
    >
      <DrawerContent>
        <DrawerTitle onClose={requestClose} toolbar={toolbarNode}>
          {titleNode}
        </DrawerTitle>
        <DrawerBody>
          {!isCreate && get.isPending ? (
            <div style={SPINNER_WRAP_STYLE}>
              <Spinner size={20} />
            </div>
          ) : (
            <form
              id={formId}
              className={formClassName}
              onSubmit={(e) => void handleSubmit(onSubmit)(e)}
            >
              {/* Render the body through a STABLE component (EditorBody) so a
                  fresh `children` closure each editor render — capturing new
                  query data etc. — does not remount the field subtree (which
                  would drop popover/open state). EditorBody calls `render(...)`
                  in its own render frame, so editors may call hooks directly
                  inside the render prop, unconditionally. */}
              <EditorBody render={children} body={bodyProps} />
            </form>
          )}
        </DrawerBody>
        <DrawerFooter>
          {footerStart !== undefined ? (
            <div style={FOOTER_START_STYLE}>{footerStartNode}</div>
          ) : null}
          <Button variant="ghost" onClick={requestClose}>
            Cancel
          </Button>
          <Button
            type="submit"
            form={formId}
            variant="primary"
            loading={submitting}
            disabled={submitting}
          >
            {isCreate ? "Create" : "Save"}
          </Button>
        </DrawerFooter>
      </DrawerContent>
      <Dialog open={confirmDiscardOpen} onOpenChange={setConfirmDiscardOpen}>
        <DialogContent>
          <DialogTitle>Discard changes?</DialogTitle>
          <DialogBody>You have unsaved changes. Closing now will discard them.</DialogBody>
          <DialogFooter>
            <Button variant="secondary" onClick={() => setConfirmDiscardOpen(false)}>
              Keep editing
            </Button>
            <Button
              variant="danger"
              onClick={() => {
                setConfirmDiscardOpen(false);
                onClose();
              }}
            >
              Discard
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </Drawer>
  );
}

/**
 * Stable wrapper that invokes the editor's render prop in its own render
 * frame. Its component type never changes across the parent's re-renders, so
 * the field subtree reconciles in place (no remount) even though `render` is
 * a fresh closure each time — and because the call happens inside a real
 * component, the render prop may call hooks directly and unconditionally.
 */
function EditorBody<Form extends FieldValues>({
  render,
  body,
}: {
  render: (body: EditorBodyProps<Form>) => ReactNode;
  body: EditorBodyProps<Form>;
}) {
  return <>{render(body)}</>;
}
