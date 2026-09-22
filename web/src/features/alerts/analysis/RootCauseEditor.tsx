// Author the "why did this fire" half of an analysis.
//
// The summary comes first and is the only required prose: it is the line the
// alert table, the inspector header and every other reader quote. Scope,
// evidence and confidence qualify it.
import { useId } from "react";
import {
  useWatch,
  type Control,
  type FieldErrors,
  type UseFormRegister,
  type UseFormSetValue,
} from "react-hook-form";
import { Input } from "@/shared/ui/Input";
import { RadioGroup, RadioOption } from "@/shared/ui/Radio";
import { Textarea } from "@/shared/ui/Textarea";
import { CharCounter } from "./CharCounter";
import { CONFIDENCE_LEVELS, confidenceLabel } from "./enums";
import { EvidenceListEditor } from "./EvidenceListEditor";
import { describeFieldError, errorAt } from "./fieldErrors";
import { ANALYSIS_LIMITS, type AnalysisForm } from "./schema";
import styles from "./editor.module.css";
import own from "./RootCauseEditor.module.css";

export type RootCauseEditorProps = {
  control: Control<AnalysisForm>;
  register: UseFormRegister<AnalysisForm>;
  setValue: UseFormSetValue<AnalysisForm>;
  errors: FieldErrors<AnalysisForm>;
  /** Re-run the resolver on every edit once the author has tried to save. */
  validateOnChange: boolean;
};

export function RootCauseEditor({
  control,
  register,
  setValue,
  errors,
  validateOnChange,
}: RootCauseEditorProps) {
  const baseId = useId();
  const summaryId = `${baseId}-summary`;
  const scopeId = `${baseId}-scope`;
  const confidenceLabelId = `${baseId}-confidence-label`;
  const confidenceHintId = `${baseId}-confidence-hint`;

  const confidence = useWatch({ control, name: "root_cause.confidence" });
  const summaryError = describeFieldError("Summary", errorAt(errors, "root_cause.summary"));
  const scopeError = describeFieldError("Scope", errorAt(errors, "root_cause.scope"));
  const confidenceError = describeFieldError(
    "Confidence",
    errorAt(errors, "root_cause.confidence"),
  );

  return (
    <section className={styles.section}>
      <h3 className={styles.sectionTitle}>Root cause</h3>

      <div className={styles.field}>
        <div className={styles.labelRow}>
          <label className={styles.label} htmlFor={summaryId}>
            Summary
          </label>
          <CharCounter
            control={control}
            name="root_cause.summary"
            limit={ANALYSIS_LIMITS.summary}
            id={`${summaryId}-count`}
          />
        </div>
        <Textarea
          id={summaryId}
          rows={3}
          placeholder="What actually went wrong, in one or two sentences."
          aria-describedby={`${summaryId}-count`}
          invalid={summaryError !== undefined}
          errorMessage={summaryError}
          {...register("root_cause.summary")}
        />
      </div>

      <div className={styles.field}>
        <div className={styles.labelRow}>
          <label className={styles.label} htmlFor={scopeId}>
            Scope
          </label>
          <CharCounter
            control={control}
            name="root_cause.scope"
            limit={ANALYSIS_LIMITS.scope}
            id={`${scopeId}-count`}
          />
        </div>
        <Input
          id={scopeId}
          placeholder="srv-victoria1:/var"
          aria-describedby={`${scopeId}-count ${scopeId}-hint`}
          invalid={scopeError !== undefined}
          errorMessage={scopeError}
          {...register("root_cause.scope")}
        />
        <p className={styles.hint} id={`${scopeId}-hint`}>
          Optional. What the cause is confined to — a host, a mount, a namespace.
        </p>
      </div>

      <EvidenceListEditor
        control={control}
        setValue={setValue}
        errors={errors}
        validateOnChange={validateOnChange}
      />

      <div className={styles.field}>
        <span className={styles.label} id={confidenceLabelId}>
          Confidence
        </span>
        {/* Radix RadioGroup items are buttons, which `<label for>` cannot
            target — each option is named through aria-labelledby instead, and
            the group keeps Radix's arrow-key roving focus. */}
        <RadioGroup
          className={own.confidence}
          value={confidence}
          aria-labelledby={confidenceLabelId}
          aria-describedby={confidenceHintId}
          onValueChange={(v) =>
            setValue("root_cause.confidence", v as AnalysisForm["root_cause"]["confidence"], {
              shouldDirty: true,
              shouldValidate: validateOnChange,
            })
          }
        >
          {CONFIDENCE_LEVELS.map((level) => (
            <span key={level} className={own.option}>
              <RadioOption value={level} aria-labelledby={`${baseId}-conf-${level}`} />
              <span className={own.optionLabel} id={`${baseId}-conf-${level}`}>
                {confidenceLabel(level)}
              </span>
            </span>
          ))}
        </RadioGroup>
        <p className={styles.hint} id={confidenceHintId}>
          Low is the honest answer for an inconclusive investigation.
        </p>
        {confidenceError !== undefined ? (
          <p className={styles.error} role="alert">
            {confidenceError}
          </p>
        ) : null}
      </div>
    </section>
  );
}
