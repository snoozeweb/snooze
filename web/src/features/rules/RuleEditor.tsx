import { useEffect, useState } from "react";
import { useForm } from "react-hook-form";
import { Button } from "@/shared/ui/Button";
import { Drawer, DrawerBody, DrawerContent, DrawerFooter, DrawerTitle } from "@/shared/ui/Drawer";
import { Input } from "@/shared/ui/Input";
import { Spinner } from "@/shared/ui/Spinner";
import { Switch } from "@/shared/ui/Switch";
import { Textarea } from "@/shared/ui/Textarea";
import { toast } from "@/shared/ui/toast/useToast";
import { ConditionEditor } from "@/shared/condition/ConditionEditor";
import { ModificationList } from "@/shared/modifications/ModificationList";
import { ApiError } from "@/lib/api/client";
import type { Condition } from "@/lib/condition/types";
import type { Modification } from "@/shared/modifications/types";
import { Rules, AggregateRules } from "./api";
import type { Rule } from "./types";
import styles from "./RuleEditor.module.css";

type FormShape = {
  name: string;
  comment: string;
  enabled: boolean;
  condition: Condition;
  modifications: Modification[];
};

const EMPTY_FORM: FormShape = {
  name: "",
  comment: "",
  enabled: true,
  condition: { type: "ALWAYS_TRUE" },
  modifications: [],
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
      reset({
        name: existing.data.name ?? "",
        comment: existing.data.comment ?? "",
        enabled: existing.data.enabled ?? true,
        condition: existing.data.condition ?? { type: "ALWAYS_TRUE" },
        modifications: existing.data.modifications ?? [],
      });
    }
  }, [existing.data, isCreate, reset]);

  const [submitting, setSubmitting] = useState(false);

  async function onSubmit(form: FormShape) {
    setSubmitting(true);
    try {
      const body: Rule = {
        name: form.name,
        ...(form.comment ? { comment: form.comment } : {}),
        enabled: form.enabled,
        condition: form.condition,
        ...(form.modifications.length > 0 ? { modifications: form.modifications } : {}),
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
  const nameInvalid = formState.isSubmitted && !watch("name").trim();
  const labelPlugin = plugin === "rule" ? "rule" : "aggregate rule";

  return (
    <Drawer
      open
      onOpenChange={(o) => {
        if (!o) onClose();
      }}
    >
      <DrawerContent>
        <DrawerTitle>{isCreate ? `New ${labelPlugin}` : `Edit ${labelPlugin}`}</DrawerTitle>
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
                <div className={styles.row}>
                  <Switch
                    checked={enabled}
                    onCheckedChange={(v) => setValue("enabled", v, { shouldDirty: true })}
                    aria-label="Enabled"
                  />
                  <span>Enabled</span>
                </div>
              </section>
              <section className={styles.section}>
                <h3 className={styles.sectionTitle}>Condition</h3>
                <ConditionEditor
                  value={condition}
                  onChange={(c) => setValue("condition", c, { shouldDirty: true })}
                  plugin="record"
                />
              </section>
              <section className={styles.section}>
                <h3 className={styles.sectionTitle}>Modifications</h3>
                <ModificationList
                  value={modifications}
                  onChange={(m) => setValue("modifications", m, { shouldDirty: true })}
                />
              </section>
            </form>
          )}
        </DrawerBody>
        <DrawerFooter>
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
