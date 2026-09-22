// Write or correct an alert's analysis, in place inside the Analysis tab.
//
// In place rather than in a drawer (a plan decision): the thing being written
// is a reading of the alert, and the alert's timeline, labels and history are
// the material. Covering them with a modal to write about them is backwards.
//
// react-hook-form owns the form state — there is no Context here and no local
// mirror of the values. The resolver in `schema.ts` and the server's 422 write
// into the same error tree, keyed by the same paths, so a failure reads the
// same whichever side caught it.
import { useState } from "react";
import { useForm, type FieldPath } from "react-hook-form";
import { Button } from "@/shared/ui/Button";
import { CollapsibleSection } from "@/shared/ui/CollapsibleSection";
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogTitle,
} from "@/shared/ui/Dialog";
import { InlineError } from "@/shared/ui/InlineError";
import { useAnnounce } from "@/shared/a11y/LiveAnnouncer";
import { toast } from "@/shared/ui/toast/useToast";
import { ApiError } from "@/lib/api/client";
import { describeError, type ErrorCopy } from "@/lib/api/errorMessage";
import { useAuth } from "@/lib/auth/store";
import type { components } from "@/lib/api/types.gen";
import { analysisFieldErrors, useSetAnalysis, type AgenticEnvelope } from "./api";
import { AutomatableSwitch } from "./AutomatableSwitch";
import { isAddressablePath, toRhfPath } from "./fieldErrors";
import { RootCauseEditor } from "./RootCauseEditor";
import { StepListEditor } from "./StepListEditor";
import {
  ANALYSIS_LIMITS,
  ANALYSIS_SOURCE,
  analysisResolver,
  analysisToForm,
  emptyAnalysisForm,
  formToRequest,
  type AnalysisForm,
  type AnalysisFormContext,
} from "./schema";
import styles from "./editor.module.css";
import own from "./AnalysisEditor.module.css";

export type Agentic = components["schemas"]["Agentic"];

/** The server's own copy for a session that lost the one permission this route wants. */
const FORBIDDEN_COPY: ErrorCopy = {
  summary: "Your session no longer holds rw_protected. Ask an admin to grant it, then retry.",
};

const GONE_COPY: ErrorCopy = { summary: "This alert no longer exists." };

export type AnalysisEditorProps = {
  /** The alert the analysis hangs off. */
  uid: string;
  /** The stored analysis being corrected, or null when authoring from scratch. */
  initial: Agentic | null;
  /** Called with the server's response — provenance already restamped. */
  onSaved: (envelope: AgenticEnvelope) => void;
  onCancel: () => void;
};

export function AnalysisEditor({ uid, initial, onSaved, onCancel }: AnalysisEditorProps) {
  const { claims } = useAuth();
  const announce = useAnnounce();
  const save = useSetAnalysis();
  const [failure, setFailure] = useState<ErrorCopy | null>(null);
  const [confirmDiscard, setConfirmDiscard] = useState(false);

  const {
    control,
    register,
    setValue,
    setError,
    handleSubmit,
    formState: { errors, isDirty, isSubmitted },
  } = useForm<AnalysisForm, AnalysisFormContext>({
    resolver: analysisResolver,
    context: { source: ANALYSIS_SOURCE },
    defaultValues: initial ? analysisToForm(initial) : emptyAnalysisForm(),
    mode: "onBlur",
  });

  // A rollback that is already written stays open; an absent one folds away —
  // most plans have none, and an empty second step list reads as work not done
  // rather than as an option.
  const [rollbackStartsEmpty] = useState(
    () => (initial?.remediation_plan?.rollback ?? []).length === 0,
  );

  const previousBy = initial?.analysis?.by ?? "";
  const previousLabel = initial?.analysis?.source ?? previousBy;
  const replacesSomeoneElse = previousBy !== "" && previousBy !== (claims?.sub ?? "");

  async function onSubmit(values: AnalysisForm) {
    setFailure(null);
    try {
      const envelope = await save.mutateAsync({
        uid,
        body: formToRequest(values, ANALYSIS_SOURCE),
      });
      announce("Analysis saved");
      toast.success("Analysis saved");
      onSaved(envelope);
    } catch (err) {
      handleFailure(err, values);
    }
  }

  function handleFailure(err: unknown, values: AnalysisForm) {
    if (err instanceof ApiError && err.status === 403) {
      // Deliberately leaves the form editable and the values intact: the fix
      // is a permission grant elsewhere, after which Save works unchanged.
      setFailure(FORBIDDEN_COPY);
      return;
    }
    if (err instanceof ApiError && err.status === 404) {
      setFailure(GONE_COPY);
      return;
    }
    // A 422 names its fields, and placing the messages ON the fields is the
    // whole point of keeping the paths identical on both sides. Only a 422
    // though: another status may carry a `details` map that means something
    // else entirely, and mining it for field paths would put a validation
    // message on a form the server never validated.
    if (err instanceof ApiError && err.status === 422) {
      const fields = Object.entries(analysisFieldErrors(err));
      const placed = fields.filter(([path]) => isAddressablePath(path, values));
      const orphans = fields.filter(([path]) => !isAddressablePath(path, values));
      placed.forEach(([path, message], i) => {
        setError(
          toRhfPath(path) as FieldPath<AnalysisForm>,
          { type: "server", message },
          // Focus the first one: a rejected save that leaves the caret where
          // it was makes the reader hunt the form for the red box.
          { shouldFocus: i === 0 },
        );
      });
      // A path no control renders would vanish into the error tree, leaving a
      // refusal that looks like a Save that never happened. Say it verbatim
      // instead — the reader cannot fix what the form will not show, but they
      // can read the sentence and report it.
      if (orphans.length > 0) {
        setFailure({
          summary: `The server rejected the payload: ${orphans
            .map(([path, message]) => `${path}: ${message}`)
            .join("; ")}`,
        });
        return;
      }
      if (placed.length > 0) return;
      // A 422 with no usable details at all falls through to the banner below,
      // which carries the server's own one-line message.
    }
    setFailure(describeError(err, "Could not save the analysis."));
  }

  function requestCancel() {
    if (isDirty) {
      setConfirmDiscard(true);
      return;
    }
    onCancel();
  }

  const stepList = (
    <StepListEditor
      control={control}
      register={register}
      setValue={setValue}
      errors={errors}
      name="remediation_plan.steps"
      heading="Steps"
      min={1}
      max={ANALYSIS_LIMITS.steps}
      addLabel="Add step"
      noun="step"
      removeDisabledHint="A plan needs at least one step"
      validateOnChange={isSubmitted}
    />
  );

  const rollbackList = (
    <StepListEditor
      control={control}
      register={register}
      setValue={setValue}
      errors={errors}
      name="remediation_plan.rollback"
      heading="Rollback"
      min={0}
      max={ANALYSIS_LIMITS.steps}
      addLabel="Add rollback step"
      noun="rollback step"
      removeDisabledHint="A rollback may be empty"
      validateOnChange={isSubmitted}
    />
  );

  return (
    <>
      <form className={styles.form} onSubmit={(e) => void handleSubmit(onSubmit)(e)} noValidate>
        <RootCauseEditor
          control={control}
          register={register}
          setValue={setValue}
          errors={errors}
          validateOnChange={isSubmitted}
        />

        <section className={styles.section}>
          <h3 className={styles.sectionTitle}>Remediation</h3>
          {stepList}
          {rollbackStartsEmpty ? (
            <CollapsibleSection title="Rollback" summary="None recorded">
              {rollbackList}
            </CollapsibleSection>
          ) : (
            rollbackList
          )}
          <AutomatableSwitch control={control} setValue={setValue} />
        </section>

        {failure !== null ? <InlineError {...failure} /> : null}

        {replacesSomeoneElse ? (
          <p className={styles.hint}>
            {`Replaces the analysis written by ${previousLabel}; provenance will show you.`}
          </p>
        ) : null}

        <div className={own.footer}>
          <Button variant="secondary" onClick={requestCancel}>
            Cancel
          </Button>
          <Button type="submit" variant="primary" loading={save.isPending}>
            Save analysis
          </Button>
        </div>
      </form>

      <Dialog open={confirmDiscard} onOpenChange={setConfirmDiscard}>
        <DialogContent>
          <DialogTitle>Discard changes?</DialogTitle>
          <DialogBody>
            <DialogDescription>
              This analysis has unsaved edits. Leaving now will discard them.
            </DialogDescription>
          </DialogBody>
          <DialogFooter>
            <Button variant="secondary" onClick={() => setConfirmDiscard(false)}>
              Keep editing
            </Button>
            <Button
              variant="danger"
              onClick={() => {
                setConfirmDiscard(false);
                onCancel();
              }}
            >
              Discard changes
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
