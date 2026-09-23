// The plan's verdict — what the alert needs from on-call right now — as a
// select in the editor.
//
// Optional on the wire, so "Not stated" is a real first option rather than an
// empty placeholder: it is what an author leaves when the plan speaks for
// itself, and it is never sent (see formToRequest).
import { useId } from "react";
import { useWatch, type Control, type FieldErrors, type UseFormSetValue } from "react-hook-form";
import { Select, SelectContent, SelectItem, SelectTrigger } from "@/shared/ui/Select";
import { describeFieldError, errorAt } from "./fieldErrors";
import type { AnalysisForm } from "./schema";
import { PLAN_STATUSES, planStatusHint, planStatusLabel } from "./verdict";
import styles from "./editor.module.css";

/** Radix Select cannot hold "" as an item value; "not stated" rides this. */
const UNSET = "unset";

export type PlanStatusSelectProps = {
  control: Control<AnalysisForm>;
  setValue: UseFormSetValue<AnalysisForm>;
  errors: FieldErrors<AnalysisForm>;
  /** Re-run the resolver on every edit once the author has tried to save. */
  validateOnChange: boolean;
};

export function PlanStatusSelect({
  control,
  setValue,
  errors,
  validateOnChange,
}: PlanStatusSelectProps) {
  const baseId = useId();
  const id = `${baseId}-status`;
  const status = useWatch({ control, name: "remediation_plan.status" });
  const error = describeFieldError("Status", errorAt(errors, "remediation_plan.status"));
  return (
    <div className={styles.field}>
      <span className={styles.label} id={`${id}-label`}>
        Status
      </span>
      <Select
        value={status === "" || status === undefined ? UNSET : status}
        onValueChange={(v) =>
          setValue("remediation_plan.status", v === UNSET ? "" : (v as typeof status), {
            shouldDirty: true,
            shouldValidate: validateOnChange,
          })
        }
      >
        <SelectTrigger
          id={id}
          aria-labelledby={`${id}-label ${id}`}
          className={styles.narrowSelect!}
        />
        <SelectContent>
          <SelectItem value={UNSET}>Not stated</SelectItem>
          {PLAN_STATUSES.map((s) => (
            <SelectItem key={s} value={s}>
              {planStatusLabel(s)}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <p className={styles.hint}>
        {status === "" || status === undefined
          ? "Optional. What the alert needs from on-call right now — shown first to whoever opens it."
          : `${planStatusHint(status)}.`}
      </p>
      {error !== undefined ? (
        <p className={styles.error} role="alert">
          {error}
        </p>
      ) : null}
    </div>
  );
}
