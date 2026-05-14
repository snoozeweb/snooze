import { useEffect, useState } from "react";
import { useForm } from "react-hook-form";
import { Button } from "@/shared/ui/Button";
import { Drawer, DrawerBody, DrawerContent, DrawerFooter, DrawerTitle } from "@/shared/ui/Drawer";
import { Input } from "@/shared/ui/Input";
import { Spinner } from "@/shared/ui/Spinner";
import { Textarea } from "@/shared/ui/Textarea";
import { toast } from "@/shared/ui/toast/useToast";
import { ApiError } from "@/lib/api/client";
import { Actions } from "./api";
import type { Action } from "./types";
import styles from "./NotificationEditor.module.css";

type FormShape = {
  name: string;
  comment: string;
  action_type: string;
  action_json: string;
};

const EMPTY_FORM: FormShape = {
  name: "",
  comment: "",
  action_type: "script",
  action_json: "{}",
};

export type ActionEditorProps = {
  uid: string | undefined;
  onClose: () => void;
};

export function ActionEditor({ uid, onClose }: ActionEditorProps) {
  const isCreate = uid === undefined || uid === "";
  const existing = Actions.useGet(isCreate ? undefined : uid);
  const create = Actions.useCreate();
  const update = Actions.useUpdate();

  const { register, handleSubmit, reset, formState, watch } = useForm<FormShape>({
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
        action_type: existing.data.action_type ?? "script",
        action_json: JSON.stringify(existing.data.action ?? {}, null, 2),
      });
    }
  }, [existing.data, isCreate, reset]);

  const [submitting, setSubmitting] = useState(false);
  const [jsonError, setJsonError] = useState<string | null>(null);

  async function onSubmit(form: FormShape) {
    setSubmitting(true);
    setJsonError(null);
    try {
      let parsed: Record<string, unknown>;
      try {
        parsed = JSON.parse(form.action_json) as Record<string, unknown>;
        if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) {
          throw new Error("expected a JSON object");
        }
      } catch (e) {
        setJsonError(e instanceof Error ? e.message : "Invalid JSON");
        setSubmitting(false);
        return;
      }
      const body: Action = {
        name: form.name,
        ...(form.comment ? { comment: form.comment } : {}),
        action_type: form.action_type,
        action: parsed,
      };
      if (isCreate) {
        await create.mutateAsync(body);
        toast.success("Action created");
      } else {
        await update.mutateAsync({ uid, body });
        toast.success("Action saved");
      }
      onClose();
    } catch (e) {
      toast.error(e instanceof ApiError ? e.detail : "Save failed");
    } finally {
      setSubmitting(false);
    }
  }

  const nameInvalid = formState.isSubmitted && !watch("name").trim();

  return (
    <Drawer
      open
      onOpenChange={(o) => {
        if (!o) onClose();
      }}
    >
      <DrawerContent>
        <DrawerTitle>{isCreate ? "New action" : "Edit action"}</DrawerTitle>
        <DrawerBody>
          {!isCreate && existing.isPending ? (
            <div style={{ display: "flex", justifyContent: "center", padding: "var(--space-5)" }}>
              <Spinner size={20} />
            </div>
          ) : (
            <form
              id="action-form"
              className={styles.stack}
              onSubmit={(e) => void handleSubmit(onSubmit)(e)}
            >
              <section className={styles.section}>
                <h3 className={styles.sectionTitle}>Identity</h3>
                <div className={styles.field}>
                  <label className={styles.label} htmlFor="action-name">
                    Name
                  </label>
                  <Input
                    id="action-name"
                    {...register("name")}
                    invalid={nameInvalid}
                    placeholder="e.g. slack-prod"
                  />
                </div>
                <div className={styles.field}>
                  <label className={styles.label} htmlFor="action-type">
                    Type
                  </label>
                  <Input
                    id="action-type"
                    {...register("action_type")}
                    placeholder="script | webhook | mail | …"
                  />
                </div>
                <div className={styles.field}>
                  <label className={styles.label} htmlFor="action-comment">
                    Comment
                  </label>
                  <Textarea id="action-comment" {...register("comment")} rows={2} />
                </div>
              </section>
              <section className={styles.section}>
                <h3 className={styles.sectionTitle}>Config (JSON)</h3>
                <Textarea
                  {...register("action_json")}
                  rows={10}
                  invalid={!!jsonError}
                  style={{ fontFamily: "var(--font-mono)" }}
                />
                {jsonError ? (
                  <span style={{ color: "var(--severity-critical)", fontSize: "var(--text-xs)" }}>
                    {jsonError}
                  </span>
                ) : null}
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
            form="action-form"
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
