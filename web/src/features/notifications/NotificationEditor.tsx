import { useEffect, useMemo, useState } from "react";
import { useForm } from "react-hook-form";
import { Button } from "@/shared/ui/Button";
import { CollapsibleSection } from "@/shared/ui/CollapsibleSection";
import { ConditionPreview } from "@/shared/ui/ConditionPreview";
import { Drawer, DrawerBody, DrawerContent, DrawerFooter, DrawerTitle } from "@/shared/ui/Drawer";
import { FrequencyEditor } from "@/shared/ui/FrequencyEditor";
import { summarizeFrequency } from "@/shared/ui/frequencyUtils";
import { Input } from "@/shared/ui/Input";
import { MultiCombobox } from "@/shared/ui/MultiCombobox";
import { Spinner } from "@/shared/ui/Spinner";
import { Switch } from "@/shared/ui/Switch";
import { Textarea } from "@/shared/ui/Textarea";
import { TimeConstraintsCell } from "@/shared/ui/TimeConstraintsCell";
import { TimeConstraintsEditor } from "@/shared/ui/TimeConstraintsEditor";
import { toast } from "@/shared/ui/toast/useToast";
import { ConditionEditor } from "@/shared/condition/ConditionEditor";
import { ApiError } from "@/lib/api/client";
import type { Condition } from "@/lib/condition/types";
import type { TimeConstraintsGroup } from "@/lib/timeconstraints/types";
import { DiffSection } from "@/shared/ui/DiffSection";
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
  const existing = Notifications.useGet(isCreate ? undefined : uid);
  const create = Notifications.useCreate();
  const update = Notifications.useUpdate();

  const { register, handleSubmit, reset, watch, setValue, formState } = useForm<FormShape>({
    defaultValues: EMPTY_FORM,
  });

  useEffect(() => {
    if (isCreate) {
      reset(EMPTY_FORM);
      return;
    }
    if (existing.data) {
      reset({
        name: existing.data.name ?? "",
        comment: existing.data.comment ?? "",
        enabled: existing.data.enabled ?? true,
        condition: existing.data.condition ?? { type: "ALWAYS_TRUE" },
        actions: existing.data.actions ?? [],
        time_constraints: existing.data.time_constraints ?? {},
        frequency: existing.data.frequency ?? {},
      });
    }
  }, [existing.data, isCreate, reset]);

  const [submitting, setSubmitting] = useState(false);

  async function onSubmit(form: FormShape) {
    setSubmitting(true);
    try {
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
      if (isCreate) {
        await create.mutateAsync(body);
        toast.success("Notification created");
      } else {
        await update.mutateAsync({ uid, body });
        toast.success("Notification saved");
      }
      onClose();
    } catch (e) {
      toast.error(e instanceof ApiError ? e.detail : "Save failed");
    } finally {
      setSubmitting(false);
    }
  }

  const condition = watch("condition");
  const enabled = watch("enabled");
  const actions = watch("actions");
  const timeConstraints = watch("time_constraints");
  const frequency = watch("frequency");
  const nameInvalid = formState.isSubmitted && !watch("name").trim();

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

  const projected: Notification = {
    name: watch("name"),
    ...(watch("comment") ? { comment: watch("comment") } : {}),
    enabled: enabled,
    condition: condition,
    ...(actions.length > 0 ? { actions } : {}),
  };

  return (
    <Drawer
      open
      onOpenChange={(o) => {
        if (!o) onClose();
      }}
    >
      <DrawerContent>
        <DrawerTitle
          toolbar={
            <>
              <Switch
                checked={enabled}
                onCheckedChange={(v) => setValue("enabled", v, { shouldDirty: true })}
                aria-label="Enabled"
              />
              <span>{enabled ? "Enabled" : "Disabled"}</span>
            </>
          }
        >
          {isCreate ? "New notification" : "Edit notification"}
        </DrawerTitle>
        <DrawerBody>
          {!isCreate && existing.isPending ? (
            <div style={{ display: "flex", justifyContent: "center", padding: "var(--space-5)" }}>
              <Spinner size={20} />
            </div>
          ) : (
            <form
              id="notif-form"
              className={styles.stack}
              onSubmit={(e) => void handleSubmit(onSubmit)(e)}
            >
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
              <CollapsibleSection
                title="Frequency"
                summary={summarizeFrequency(frequency)}
                defaultOpen
              >
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
            </form>
          )}
        </DrawerBody>
        <DrawerFooter>
          <div style={{ flex: 1 }}>
            <DiffSection original={isCreate ? undefined : existing.data} current={projected} />
          </div>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button
            type="submit"
            form="notif-form"
            variant="primary"
            loading={submitting}
            disabled={submitting}
          >
            {isCreate ? "Create" : "Save"}
          </Button>
        </DrawerFooter>
      </DrawerContent>
    </Drawer>
  );
}
