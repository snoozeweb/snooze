import { useMemo, type ReactNode } from "react";
import { useWatch, type Control, type UseFormSetValue } from "react-hook-form";
import { CollapsibleSection } from "@/shared/ui/CollapsibleSection";
import { ConditionPreview } from "@/shared/ui/ConditionPreview";
import { FrequencyEditor } from "@/shared/ui/FrequencyEditor";
import { summarizeFrequency } from "@/shared/ui/frequencyUtils";
import { Input } from "@/shared/ui/Input";
import { MultiCombobox } from "@/shared/ui/MultiCombobox";
import { Switch } from "@/shared/ui/Switch";
import { Textarea } from "@/shared/ui/Textarea";
import { TimeConstraintsCell } from "@/shared/ui/TimeConstraintsCell";
import { TimeConstraintsEditor } from "@/shared/ui/TimeConstraintsEditor";
import { ConditionEditor } from "@/shared/condition/ConditionEditor";
import type { Condition } from "@/lib/condition/types";
import type { TimeConstraintsGroup } from "@/lib/timeconstraints/types";
import { DiffSection } from "@/shared/ui/DiffSection";
import { EditorDrawer, useFieldInvalid, type EditorBodyProps } from "@/shared/forms/EditorDrawer";
import { Actions, Notifications } from "./api";
import type { Frequency, Notification } from "./types";
import styles from "./NotificationEditor.module.css";

type FormShape = {
  name: string;
  comment: string;
  enabled: boolean;
  condition: Condition;
  actions: string[];
  time_constraints: TimeConstraintsGroup;
  frequency: Frequency;
};

const EMPTY_FORM: FormShape = {
  name: "",
  comment: "",
  enabled: true,
  condition: { type: "ALWAYS_TRUE" },
  actions: [],
  time_constraints: {},
  frequency: {},
};

export type NotificationEditorProps = {
  uid: string | undefined;
  onClose: () => void;
};

export function NotificationEditor({ uid, onClose }: NotificationEditorProps) {
  const isCreate = uid === undefined || uid === "";
  const get = Notifications.useGet(isCreate ? undefined : uid);
  const create = Notifications.useCreate();
  const update = Notifications.useUpdate();

  return (
    <EditorDrawer<FormShape, Notification>
      uid={uid}
      onClose={onClose}
      get={get}
      create={create}
      update={update}
      emptyForm={EMPTY_FORM}
      recordToForm={(n) => ({
        name: n.name ?? "",
        comment: n.comment ?? "",
        enabled: n.enabled ?? true,
        condition: n.condition ?? { type: "ALWAYS_TRUE" },
        actions: n.actions ?? [],
        time_constraints: n.time_constraints ?? {},
        frequency: n.frequency ?? {},
      })}
      formToBody={(form) => {
        const hasTimeConstraints =
          (form.time_constraints.datetime?.length ?? 0) > 0 ||
          (form.time_constraints.time?.length ?? 0) > 0 ||
          (form.time_constraints.weekdays?.length ?? 0) > 0;
        const hasFrequency =
          (form.frequency.total ?? 0) > 0 ||
          (form.frequency.delay ?? 0) > 0 ||
          (form.frequency.every ?? 0) > 0;
        const body: Notification = {
          name: form.name,
          ...(form.comment ? { comment: form.comment } : {}),
          enabled: form.enabled,
          condition: form.condition,
          ...(form.actions.length > 0 ? { actions: form.actions } : {}),
          ...(hasTimeConstraints ? { time_constraints: form.time_constraints } : {}),
          ...(hasFrequency ? { frequency: form.frequency } : {}),
        };
        return body;
      }}
      title={(c): ReactNode => (c ? "New notification" : "Edit notification")}
      titleToolbar={({ control, setValue }) => (
        <NotificationEnabledToggle control={control} setValue={setValue} />
      )}
      footerStart={({ control }) => (
        <NotificationDiff control={control} original={isCreate ? undefined : get.data} />
      )}
      successMessage={{ create: "Notification created", update: "Notification saved" }}
      formId="notif-form"
      formClassName={styles.stack}
    >
      {(body) => <NotificationFields {...body} />}
    </EditorDrawer>
  );
}

/** Enabled switch scoped to its own subscription. */
function NotificationEnabledToggle({
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

/** Diff scoped to its own subscriptions. */
function NotificationDiff({
  control,
  original,
}: {
  control: Control<FormShape>;
  original: Notification | undefined;
}) {
  const name = useWatch({ control, name: "name" });
  const comment = useWatch({ control, name: "comment" });
  const enabled = useWatch({ control, name: "enabled" });
  const condition = useWatch({ control, name: "condition" });
  const actions = useWatch({ control, name: "actions" });
  const projected: Notification = {
    name,
    ...(comment ? { comment } : {}),
    enabled,
    condition,
    ...(actions.length > 0 ? { actions } : {}),
  };
  return <DiffSection original={original} current={projected} />;
}

function NotificationFields({ control, register, setValue }: EditorBodyProps<FormShape>) {
  const nameInvalid = useFieldInvalid(control, "name");
  const condition = useWatch({ control, name: "condition" });
  const actions = useWatch({ control, name: "actions" });
  const timeConstraints = useWatch({ control, name: "time_constraints" });
  const frequency = useWatch({ control, name: "frequency" });

  // Action options come from the API — fall back to whatever's already
  // on the notification so an existing reference is preserved even when
  // the action list hasn't loaded yet.
  const actionsList = Actions.useList({ limit: 500 });
  const actionOptions = useMemo(() => {
    const available = (actionsList.data?.data ?? []).map((a) => ({
      value: a.name,
      label: a.name,
    }));
    const known = new Set(available.map((o) => o.value));
    const merged = [...available];
    for (const a of actions) {
      if (!known.has(a)) merged.push({ value: a, label: a });
    }
    return merged;
  }, [actionsList.data, actions]);

  return (
    <>
      <section className={styles.section}>
        <h3 className={styles.sectionTitle}>Identity</h3>
        <div className={styles.grid2}>
          <div className={styles.field}>
            <label className={styles.label} htmlFor="notif-name">
              Name
            </label>
            <Input
              id="notif-name"
              {...register("name")}
              invalid={nameInvalid}
              placeholder="e.g. page-on-call"
            />
          </div>
          <div className={styles.field}>
            <span className={styles.label}>Actions</span>
            <MultiCombobox
              aria-label="Actions"
              placeholder="Select one or more action names"
              options={actionOptions}
              value={actions}
              onChange={(next) => setValue("actions", next, { shouldDirty: true })}
              allowCustom
            />
          </div>
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
      <CollapsibleSection
        title="Time constraints"
        summary={<TimeConstraintsCell value={timeConstraints} />}
        defaultOpen
      >
        <TimeConstraintsEditor
          value={timeConstraints}
          onChange={(g) => setValue("time_constraints", g, { shouldDirty: true })}
        />
      </CollapsibleSection>
      <CollapsibleSection title="Frequency" summary={summarizeFrequency(frequency)} defaultOpen>
        <FrequencyEditor
          value={frequency}
          onChange={(f) => setValue("frequency", f, { shouldDirty: true })}
        />
      </CollapsibleSection>
      <div className={styles.field}>
        <label className={styles.label} htmlFor="notif-comment">
          Comment
        </label>
        <Textarea
          id="notif-comment"
          {...register("comment")}
          rows={2}
          placeholder="Optional description"
        />
      </div>
    </>
  );
}
