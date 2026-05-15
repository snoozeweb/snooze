import { useEffect, useState } from "react";
import { useForm } from "react-hook-form";
import { Button } from "@/shared/ui/Button";
import { CollapsibleSection } from "@/shared/ui/CollapsibleSection";
import { ConditionPreview } from "@/shared/ui/ConditionPreview";
import { Drawer, DrawerBody, DrawerContent, DrawerFooter, DrawerTitle } from "@/shared/ui/Drawer";
import { Spinner } from "@/shared/ui/Spinner";
import { Switch } from "@/shared/ui/Switch";
import { Textarea } from "@/shared/ui/Textarea";
import { Input } from "@/shared/ui/Input";
import { TimeConstraintsCell } from "@/shared/ui/TimeConstraintsCell";
import { TimeConstraintsEditor } from "@/shared/ui/TimeConstraintsEditor";
import { toast } from "@/shared/ui/toast/useToast";
import { ConditionEditor } from "@/shared/condition/ConditionEditor";
import { ApiError } from "@/lib/api/client";
import type { Condition } from "@/lib/condition/types";
import type { TimeConstraintsGroup } from "@/lib/timeconstraints/types";
import { DiffSection } from "@/shared/ui/DiffSection";
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
  const existing = Snoozes.useGet(isCreate ? undefined : uid);
  const create = Snoozes.useCreate();
  const update = Snoozes.useUpdate();

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
        time_constraints: existing.data.time_constraints ?? {},
        discard: existing.data.discard ?? false,
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
      const body: Snooze = {
        name: form.name,
        ...(form.comment ? { comment: form.comment } : {}),
        enabled: form.enabled,
        condition: form.condition,
        ...(hasTimeConstraints ? { time_constraints: form.time_constraints } : {}),
        ...(form.discard ? { discard: true } : {}),
      };
      if (isCreate) {
        await create.mutateAsync(body);
        toast.success("Snooze created");
      } else {
        await update.mutateAsync({ uid, body });
        toast.success("Snooze saved");
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
  const tc = watch("time_constraints");
  const discard = watch("discard");
  const nameInvalid = formState.isSubmitted && !watch("name").trim();

  const projected: Snooze = {
    name: watch("name"),
    ...(watch("comment") ? { comment: watch("comment") } : {}),
    enabled: enabled,
    condition: condition,
    ...(discard ? { discard: true } : {}),
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
          {isCreate ? "New snooze" : "Edit snooze"}
        </DrawerTitle>
        <DrawerBody>
          {!isCreate && existing.isPending ? (
            <div style={{ display: "flex", justifyContent: "center", padding: "var(--space-5)" }}>
              <Spinner size={20} />
            </div>
          ) : (
            <form
              id="snooze-form"
              className={styles.stack}
              onSubmit={(e) => void handleSubmit(onSubmit)(e)}
            >
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
                defaultOpen={
                  (tc.weekdays?.length ?? 0) > 0 ||
                  (tc.time?.length ?? 0) > 0 ||
                  (tc.datetime?.length ?? 0) > 0
                }
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
            form="snooze-form"
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
