import { useWatch, type Control, type UseFormSetValue } from "react-hook-form";
import { ConditionPreview } from "@/shared/ui/ConditionPreview";
import { Switch } from "@/shared/ui/Switch";
import { Input } from "@/shared/ui/Input";
import { ConditionEditor } from "@/shared/condition/ConditionEditor";
import type { Condition } from "@/lib/condition/types";
import { DiffSection } from "@/shared/ui/DiffSection";
import { EditorDrawer, useFieldInvalid, type EditorBodyProps } from "@/shared/forms/EditorDrawer";
import { Reject } from "./api";
import type { RejectRule } from "./types";
import styles from "./RejectEditor.module.css";

type FormShape = {
  name: string;
  enabled: boolean;
  condition: Condition;
};

const EMPTY_FORM: FormShape = {
  name: "",
  enabled: true,
  condition: { type: "ALWAYS_TRUE" },
};

export type RejectEditorProps = {
  uid: string | undefined;
  onClose: () => void;
};

/**
 * RejectEditor — create/edit drawer for a reject rule. A reject rule is the
 * simplest of the three Rules-page editors: name + enabled toggle + condition
 * only (no modifications, no aggregate fields, no time constraints). Mirrors
 * SnoozeEditor's wiring minus the time-constraint section.
 */
export function RejectEditor({ uid, onClose }: RejectEditorProps) {
  const isCreate = uid === undefined || uid === "";
  const get = Reject.useGet(isCreate ? undefined : uid);
  const create = Reject.useCreate();
  const update = Reject.useUpdate();

  return (
    <EditorDrawer<FormShape, RejectRule>
      uid={uid}
      onClose={onClose}
      get={get}
      create={create}
      update={update}
      emptyForm={EMPTY_FORM}
      recordToForm={(r) => ({
        name: r.name ?? "",
        enabled: r.enabled ?? true,
        condition: r.condition ?? { type: "ALWAYS_TRUE" },
      })}
      formToBody={(form) => ({
        name: form.name,
        enabled: form.enabled,
        condition: form.condition,
      })}
      title={(c) => (c ? "New reject rule" : "Edit reject rule")}
      titleToolbar={({ control, setValue }) => (
        <RejectEnabledToggle control={control} setValue={setValue} />
      )}
      footerStart={({ control }) => (
        <RejectDiff control={control} original={isCreate ? undefined : get.data} />
      )}
      successMessage={{ create: "Reject rule created", update: "Reject rule saved" }}
      formId="reject-form"
      formClassName={styles.stack}
    >
      {(body) => <RejectFields {...body} />}
    </EditorDrawer>
  );
}

/** Enabled switch scoped to its own `enabled` subscription. */
function RejectEnabledToggle({
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

/** Diff scoped to its own subscriptions, mirroring SnoozeEditor's SnoozeDiff. */
function RejectDiff({
  control,
  original,
}: {
  control: Control<FormShape>;
  original: RejectRule | undefined;
}) {
  const name = useWatch({ control, name: "name" });
  const enabled = useWatch({ control, name: "enabled" });
  const condition = useWatch({ control, name: "condition" });
  const projected: RejectRule = { name, enabled, condition };
  return <DiffSection original={original} current={projected} />;
}

function RejectFields({ control, register, setValue }: EditorBodyProps<FormShape>) {
  const nameInvalid = useFieldInvalid(control, "name");
  const condition = useWatch({ control, name: "condition" });

  return (
    <>
      <section className={styles.section}>
        <h3 className={styles.sectionTitle}>Identity</h3>
        <div className={styles.field}>
          <label className={styles.label} htmlFor="reject-name">
            Name
          </label>
          <Input
            id="reject-name"
            {...register("name")}
            invalid={nameInvalid}
            placeholder="e.g. block-legacy-sources"
          />
        </div>
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
    </>
  );
}
