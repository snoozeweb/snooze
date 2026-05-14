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
import { Notifications } from "./api";
import type { Notification } from "./types";
import styles from "./NotificationEditor.module.css";

type FormShape = {
  name: string;
  comment: string;
  enabled: boolean;
  condition: Condition;
  actions: string;
};

const EMPTY_FORM: FormShape = {
  name: "",
  comment: "",
  enabled: true,
  condition: { type: "ALWAYS_TRUE" },
  actions: "",
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
        actions: (existing.data.actions ?? []).join(", "),
      });
    }
  }, [existing.data, isCreate, reset]);

  const [submitting, setSubmitting] = useState(false);

  async function onSubmit(form: FormShape) {
    setSubmitting(true);
    try {
      const actionsArr = form.actions
        .split(",")
        .map((s) => s.trim())
        .filter((s) => s.length > 0);
      const body: Notification = {
        name: form.name,
        ...(form.comment ? { comment: form.comment } : {}),
        enabled: form.enabled,
        condition: form.condition,
        ...(actionsArr.length > 0 ? { actions: actionsArr } : {}),
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
  const nameInvalid = formState.isSubmitted && !watch("name").trim();

  return (
    <Drawer
      open
      onOpenChange={(o) => {
        if (!o) onClose();
      }}
    >
      <DrawerContent>
        <DrawerTitle>{isCreate ? "New notification" : "Edit notification"}</DrawerTitle>
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
                <div className={styles.field}>
                  <label className={styles.label} htmlFor="notif-actions">
                    Actions (comma-separated action names)
                  </label>
                  <Input
                    id="notif-actions"
                    {...register("actions")}
                    placeholder="e.g. slack-prod, pagerduty"
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
