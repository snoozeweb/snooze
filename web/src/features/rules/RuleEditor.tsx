import { useEffect, useState } from "react";
import { useForm } from "react-hook-form";
import { Button } from "@/shared/ui/Button";
import { ConditionPreview } from "@/shared/ui/ConditionPreview";
import { Drawer, DrawerBody, DrawerContent, DrawerFooter, DrawerTitle } from "@/shared/ui/Drawer";
import { DurationInput } from "@/shared/ui/DurationInput";
import { Input } from "@/shared/ui/Input";
import { MultiCombobox } from "@/shared/ui/MultiCombobox";
import { Spinner } from "@/shared/ui/Spinner";
import { Switch } from "@/shared/ui/Switch";
import { Textarea } from "@/shared/ui/Textarea";
import { toast } from "@/shared/ui/toast/useToast";
import { ConditionEditor } from "@/shared/condition/ConditionEditor";
import { ModificationList } from "@/shared/modifications/ModificationList";
import { ApiError } from "@/lib/api/client";
import type { Condition } from "@/lib/condition/types";
import type { Modification } from "@/shared/modifications/types";
import {
  modificationsFromWire,
  modificationsToWire,
} from "@/shared/modifications/wire";
import { DiffSection } from "@/shared/ui/DiffSection";
import { Rules, AggregateRules } from "./api";
import type { AggregateRule, Rule } from "./types";
import styles from "./RuleEditor.module.css";

type FormShape = {
  name: string;
  comment: string;
  enabled: boolean;
  condition: Condition;
  modifications: Modification[];
  fields: string[];
  watch: string[];
  throttle: number;
};

const EMPTY_FORM: FormShape = {
  name: "",
  comment: "",
  enabled: true,
  condition: { type: "ALWAYS_TRUE" },
  modifications: [],
  fields: [],
  watch: [],
  throttle: 0,
};

export type RuleEditorProps = {
  plugin: "rule" | "aggregaterule";
  uid: string | undefined;
  onClose: () => void;
};

export function RuleEditor({ plugin, uid, onClose }: RuleEditorProps) {
  const isCreate = uid === undefined || uid === "";
  const resource = plugin === "rule" ? Rules : AggregateRules;
  const existing = resource.useGet(isCreate ? undefined : uid);
  const create = resource.useCreate();
  const update = resource.useUpdate();

  const { register, handleSubmit, reset, watch, setValue, formState } = useForm<FormShape>({
    defaultValues: EMPTY_FORM,
  });

  useEffect(() => {
    if (isCreate) {
      reset(EMPTY_FORM);
      return;
    }
    if (existing.data) {
      const agg = existing.data as AggregateRule;
      reset({
        name: existing.data.name ?? "",
        comment: existing.data.comment ?? "",
        enabled: existing.data.enabled ?? true,
        condition: existing.data.condition ?? { type: "ALWAYS_TRUE" },
        modifications: modificationsFromWire(existing.data.modifications),
        fields: agg.fields ?? [],
        watch: agg.watch ?? [],
        throttle: agg.throttle ?? 0,
      });
    }
  }, [existing.data, isCreate, reset]);

  const [submitting, setSubmitting] = useState(false);

  async function onSubmit(form: FormShape) {
    if (!form.name.trim()) {
      // RHF's `register` is used without rules; enforce a non-empty name at
      // submit so the inline invalid state has time to render before any
      // network round-trip closes the drawer.
      toast.error("Name is required");
      return;
    }
    setSubmitting(true);
    try {
      const isAggregate = plugin === "aggregaterule";
      const body: Rule & Partial<AggregateRule> = {
        name: form.name,
        ...(form.comment ? { comment: form.comment } : {}),
        enabled: form.enabled,
        condition: form.condition,
        ...(form.modifications.length > 0
          ? { modifications: modificationsToWire(form.modifications) }
          : {}),
        ...(isAggregate && form.fields.length > 0 ? { fields: form.fields } : {}),
        ...(isAggregate && form.watch.length > 0 ? { watch: form.watch } : {}),
        ...(isAggregate && form.throttle > 0 ? { throttle: form.throttle } : {}),
      };
      if (isCreate) {
        await create.mutateAsync(body);
        toast.success("Created");
      } else {
        await update.mutateAsync({ uid, body });
        toast.success("Saved");
      }
      onClose();
    } catch (e) {
      toast.error(e instanceof ApiError ? e.detail : "Save failed");
    } finally {
      setSubmitting(false);
    }
  }

  const condition = watch("condition");
  const modifications = watch("modifications");
  const enabled = watch("enabled");
  const fields = watch("fields");
  const watchFields = watch("watch");
  const throttle = watch("throttle");
  const isAggregate = plugin === "aggregaterule";
  const nameInvalid = formState.isSubmitted && !watch("name").trim();
  const labelPlugin = plugin === "rule" ? "rule" : "aggregate rule";

  // The Diff section compares against the server payload, so the projected
  // value must use the same wire shape (positional modifications).
  const projected: Rule = {
    name: watch("name"),
    ...(watch("comment") ? { comment: watch("comment") } : {}),
    enabled: enabled,
    condition: condition,
    ...(modifications.length > 0
      ? { modifications: modificationsToWire(modifications) }
      : {}),
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
          {isCreate ? `New ${labelPlugin}` : `Edit ${labelPlugin}`}
        </DrawerTitle>
        <DrawerBody>
          {!isCreate && existing.isPending ? (
            <div
              style={{
                display: "flex",
                justifyContent: "center",
                padding: "var(--space-5)",
              }}
            >
              <Spinner size={20} />
            </div>
          ) : (
            <form
              id="rule-form"
              className={styles.stack}
              onSubmit={(e) => void handleSubmit(onSubmit)(e)}
            >
              <section className={styles.section}>
                <h3 className={styles.sectionTitle}>Identity</h3>
                <div className={styles.field}>
                  <label className={styles.label} htmlFor="rule-name">
                    Name
                  </label>
                  <Input
                    id="rule-name"
                    {...register("name")}
                    invalid={nameInvalid}
                    placeholder="e.g. tag-prod-hosts"
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
              <section className={styles.section}>
                <h3 className={styles.sectionTitle}>Modifications</h3>
                <ModificationList
                  value={modifications}
                  onChange={(m) => setValue("modifications", m, { shouldDirty: true })}
                />
              </section>
              {isAggregate ? (
                <section className={styles.section}>
                  <h3 className={styles.sectionTitle}>Aggregation</h3>
                  <div className={styles.field}>
                    <span className={styles.label}>Fields (group key)</span>
                    <MultiCombobox
                      aria-label="Aggregation fields"
                      placeholder="e.g. host, source"
                      options={[]}
                      value={fields}
                      onChange={(next) => setValue("fields", next, { shouldDirty: true })}
                      allowCustom
                    />
                  </div>
                  <div className={styles.field}>
                    <span className={styles.label}>Watch (fields tracked for changes)</span>
                    <MultiCombobox
                      aria-label="Watch fields"
                      placeholder="e.g. severity, state"
                      options={[]}
                      value={watchFields}
                      onChange={(next) => setValue("watch", next, { shouldDirty: true })}
                      allowCustom
                    />
                  </div>
                  <div className={styles.field}>
                    <label className={styles.label} htmlFor="rule-throttle">
                      Throttle (0 = unlimited)
                    </label>
                    <DurationInput
                      id="rule-throttle"
                      value={throttle}
                      onChange={(n) => setValue("throttle", n, { shouldDirty: true })}
                    />
                  </div>
                </section>
              ) : null}
              <div className={styles.field}>
                <label className={styles.label} htmlFor="rule-comment">
                  Comment
                </label>
                <Textarea
                  id="rule-comment"
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
            form="rule-form"
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
