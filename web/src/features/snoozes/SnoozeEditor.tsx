import { useWatch, type Control, type UseFormSetValue } from "react-hook-form";
import { CollapsibleSection } from "@/shared/ui/CollapsibleSection";
import { ConditionPreview } from "@/shared/ui/ConditionPreview";
import { Switch } from "@/shared/ui/Switch";
import { Textarea } from "@/shared/ui/Textarea";
import { Input } from "@/shared/ui/Input";
import { TimeConstraintsCell } from "@/shared/ui/TimeConstraintsCell";
import { TimeConstraintsEditor } from "@/shared/ui/TimeConstraintsEditor";
import { ConditionEditor } from "@/shared/condition/ConditionEditor";
import type { Condition } from "@/lib/condition/types";
import type { TimeConstraintsGroup } from "@/lib/timeconstraints/types";
import { DiffSection } from "@/shared/ui/DiffSection";
import { EditorDrawer, useFieldInvalid, type EditorBodyProps } from "@/shared/forms/EditorDrawer";
import { Snoozes } from "./api";
import type { Snooze } from "./types";
import styles from "./SnoozeEditor.module.css";

type FormShape = {
  name: string;
  comment: string;
  enabled: boolean;
  condition: Condition;
  time_constraints: TimeConstraintsGroup;
  discard: boolean;
};

const EMPTY_FORM: FormShape = {
  name: "",
  comment: "",
  enabled: true,
  condition: { type: "ALWAYS_TRUE" },
  time_constraints: {},
  discard: false,
};

export type SnoozeEditorProps = {
  uid: string | undefined;
  onClose: () => void;
};

export function SnoozeEditor({ uid, onClose }: SnoozeEditorProps) {
  const isCreate = uid === undefined || uid === "";
  const get = Snoozes.useGet(isCreate ? undefined : uid);
  const create = Snoozes.useCreate();
  const update = Snoozes.useUpdate();

  return (
    <EditorDrawer<FormShape, Snooze>
      uid={uid}
      onClose={onClose}
      get={get}
      create={create}
      update={update}
      emptyForm={EMPTY_FORM}
      recordToForm={(s) => ({
        name: s.name ?? "",
        comment: s.comment ?? "",
        enabled: s.enabled ?? true,
        condition: s.condition ?? { type: "ALWAYS_TRUE" },
        time_constraints: s.time_constraints ?? {},
        discard: s.discard ?? false,
      })}
      formToBody={(form) => {
        const hasTimeConstraints =
          (form.time_constraints.datetime?.length ?? 0) > 0 ||
          (form.time_constraints.time?.length ?? 0) > 0 ||
          (form.time_constraints.weekdays?.length ?? 0) > 0;
        const body: Snooze = {
          name: form.name,
          ...(form.comment ? { comment: form.comment } : {}),
          enabled: form.enabled,
          condition: form.condition,
          ...(hasTimeConstraints ? { time_constraints: form.time_constraints } : {}),
          ...(form.discard ? { discard: true } : {}),
        };
        return body;
      }}
      title={(c) => (c ? "New snooze" : "Edit snooze")}
      titleToolbar={({ control, setValue }) => (
        <SnoozeEnabledToggle control={control} setValue={setValue} />
      )}
      footerStart={({ control }) => (
        <SnoozeDiff control={control} original={isCreate ? undefined : get.data} />
      )}
      successMessage={{ create: "Snooze created", update: "Snooze saved" }}
      formId="snooze-form"
      formClassName={styles.stack}
    >
      {(body) => <SnoozeFields {...body} />}
    </EditorDrawer>
  );
}

/** Enabled switch scoped to its own `enabled` subscription — toggling
 *  re-renders only this slot, not the whole drawer. */
function SnoozeEnabledToggle({
  control,
  setValue,
}: {
  control: Control<FormShape>;
  setValue: UseFormSetValue<FormShape>;
}) {
  const enabled = useWatch({ control, name: "enabled" });
  return (
    <>
      <Switch
        checked={enabled}
        onCheckedChange={(v) => setValue("enabled", v, { shouldDirty: true })}
        aria-label="Enabled"
      />
      <span>{enabled ? "Enabled" : "Disabled"}</span>
    </>
  );
}

/** Diff scoped to its own subscriptions, mirroring RuleEditor's RuleDiff. */
function SnoozeDiff({
  control,
  original,
}: {
  control: Control<FormShape>;
  original: Snooze | undefined;
}) {
  const name = useWatch({ control, name: "name" });
  const comment = useWatch({ control, name: "comment" });
  const enabled = useWatch({ control, name: "enabled" });
  const condition = useWatch({ control, name: "condition" });
  const discard = useWatch({ control, name: "discard" });
  const projected: Snooze = {
    name,
    ...(comment ? { comment } : {}),
    enabled,
    condition,
    ...(discard ? { discard: true } : {}),
  };
  return <DiffSection original={original} current={projected} />;
}

function SnoozeFields({ control, register, setValue }: EditorBodyProps<FormShape>) {
  const nameInvalid = useFieldInvalid(control, "name");
  const condition = useWatch({ control, name: "condition" });
  const tc = useWatch({ control, name: "time_constraints" });
  const discard = useWatch({ control, name: "discard" });

  return (
    <>
      <section className={styles.section}>
        <h3 className={styles.sectionTitle}>Identity</h3>
        <div className={styles.field}>
          <label className={styles.label} htmlFor="snooze-name">
            Name
          </label>
          <Input
            id="snooze-name"
            {...register("name")}
            invalid={nameInvalid}
            placeholder="e.g. quiet-friday-night"
          />
        </div>
        <span className={styles.row}>
          <Switch
            checked={discard}
            onCheckedChange={(v) => setValue("discard", v, { shouldDirty: true })}
            aria-label="Discard"
          />
          <span>Discard matching alerts (drop instead of tag)</span>
        </span>
      </section>
      <section className={styles.section}>
        <h3 className={styles.sectionTitle}>Condition</h3>
        <ConditionEditor
          value={condition}
          onChange={(c) => setValue("condition", c, { shouldDirty: true })}
          plugin="record"
        />
        <div style={{ marginTop: "var(--space-2)" }}>
          <ConditionPreview condition={condition} />
        </div>
      </section>
      <CollapsibleSection
        title="Time constraints"
        summary={<TimeConstraintsCell value={tc} />}
        defaultOpen
      >
        <TimeConstraintsEditor
          value={tc}
          onChange={(g) => setValue("time_constraints", g, { shouldDirty: true })}
        />
      </CollapsibleSection>
      <div className={styles.field}>
        <label className={styles.label} htmlFor="snooze-comment">
          Comment
        </label>
        <Textarea
          id="snooze-comment"
          {...register("comment")}
          rows={2}
          placeholder="Optional description"
        />
      </div>
    </>
  );
}
