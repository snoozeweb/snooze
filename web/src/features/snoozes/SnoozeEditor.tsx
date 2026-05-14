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
import { ApiError } from "@/lib/api/client";
import type { Condition } from "@/lib/condition/types";
import { Snoozes } from "./api";
import type { Snooze } from "./types";
import styles from "./SnoozeEditor.module.css";

type FormShape = {
  name: string;
  comment: string;
  enabled: boolean;
  condition: Condition;
  ttl: number;
};

const EMPTY_FORM: FormShape = {
  name: "",
  comment: "",
  enabled: true,
  condition: { type: "ALWAYS_TRUE" },
  ttl: 3600,
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
        ttl: existing.data.ttl ?? 0,
      });
    }
  }, [existing.data, isCreate, reset]);

  const [submitting, setSubmitting] = useState(false);

  async function onSubmit(form: FormShape) {
    setSubmitting(true);
    try {
      const body: Snooze = {
        name: form.name,
        ...(form.comment ? { comment: form.comment } : {}),
        enabled: form.enabled,
        condition: form.condition,
        ttl: form.ttl,
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
  const ttl = watch("ttl");
  const nameInvalid = formState.isSubmitted && !watch("name").trim();

  return (
    <Drawer
      open
      onOpenChange={(o) => {
        if (!o) onClose();
      }}
    >
      <DrawerContent>
        <DrawerTitle>{isCreate ? "New snooze" : "Edit snooze"}</DrawerTitle>
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
                <div className={styles.field}>
                  <label className={styles.label} htmlFor="snooze-ttl">
                    TTL (seconds, 0 = forever)
                  </label>
                  <Input
                    id="snooze-ttl"
                    type="number"
                    value={String(ttl)}
                    onChange={(e) =>
                      setValue("ttl", Number(e.target.value) || 0, { shouldDirty: true })
                    }
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
            </form>
          )}
        </DrawerBody>
        <DrawerFooter>
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
