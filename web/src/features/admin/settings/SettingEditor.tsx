import { useEffect, useState } from "react";
import { useForm } from "react-hook-form";
import { Button } from "@/shared/ui/Button";
import { Drawer, DrawerBody, DrawerContent, DrawerFooter, DrawerTitle } from "@/shared/ui/Drawer";
import { Input } from "@/shared/ui/Input";
import { Spinner } from "@/shared/ui/Spinner";
import { Textarea } from "@/shared/ui/Textarea";
import { toast } from "@/shared/ui/toast/useToast";
import { ApiError } from "@/lib/api/client";
import { Settings } from "./api";
import type { Setting } from "./types";
import styles from "./SettingEditor.module.css";

type FormShape = {
  name: string;
  value_json: string;
  comment: string;
};

const EMPTY_FORM: FormShape = {
  name: "",
  value_json: "null",
  comment: "",
};

export type SettingEditorProps = {
  uid: string | undefined;
  onClose: () => void;
};

export function SettingEditor({ uid, onClose }: SettingEditorProps) {
  const isCreate = uid === undefined || uid === "";
  const existing = Settings.useGet(isCreate ? undefined : uid);
  const create = Settings.useCreate();
  const update = Settings.useUpdate();

  const { register, handleSubmit, reset, formState } = useForm<FormShape>({
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
        value_json: JSON.stringify(existing.data.value ?? null, null, 2),
        comment: existing.data.comment ?? "",
      });
    }
  }, [existing.data, isCreate, reset]);

  const [submitting, setSubmitting] = useState(false);
  const [jsonError, setJsonError] = useState<string | null>(null);

  async function onSubmit(form: FormShape) {
    setSubmitting(true);
    setJsonError(null);
    let parsedValue: unknown;
    try {
      parsedValue = JSON.parse(form.value_json) as unknown;
    } catch (e) {
      setJsonError(e instanceof Error ? e.message : "Invalid JSON");
      setSubmitting(false);
      return;
    }
    try {
      const body: Setting = {
        name: form.name,
        value: parsedValue,
        ...(form.comment ? { comment: form.comment } : {}),
      };
      if (isCreate) {
        await create.mutateAsync(body);
        toast.success("Setting created");
      } else {
        await update.mutateAsync({ uid, body });
        toast.success("Setting saved");
      }
      onClose();
    } catch (e) {
      toast.error(e instanceof ApiError ? e.detail : "Save failed");
    } finally {
      setSubmitting(false);
    }
  }

  const nameInvalid = formState.isSubmitted && !formState.defaultValues?.name?.trim() && isCreate;

  return (
    <Drawer
      open
      onOpenChange={(o) => {
        if (!o) onClose();
      }}
    >
      <DrawerContent>
        <DrawerTitle>{isCreate ? "New setting" : "Edit setting"}</DrawerTitle>
        <DrawerBody>
          {!isCreate && existing.isPending ? (
            <div style={{ display: "flex", justifyContent: "center", padding: "var(--space-5)" }}>
              <Spinner size={20} />
            </div>
          ) : (
            <form
              id="setting-form"
              className={styles.stack}
              onSubmit={(e) => void handleSubmit(onSubmit)(e)}
            >
              <section className={styles.section}>
                <h3 className={styles.sectionTitle}>Identity</h3>
                <div className={styles.field}>
                  <label className={styles.label} htmlFor="setting-name">
                    Name
                  </label>
                  <Input
                    id="setting-name"
                    {...register("name")}
                    invalid={nameInvalid}
                    disabled={!isCreate}
                    placeholder="e.g. max_retries"
                  />
                </div>
                <div className={styles.field}>
                  <label className={styles.label} htmlFor="setting-comment">
                    Comment
                  </label>
                  <Textarea id="setting-comment" {...register("comment")} rows={2} />
                </div>
              </section>
              <section className={styles.section}>
                <h3 className={styles.sectionTitle}>Value (JSON)</h3>
                <Textarea
                  {...register("value_json")}
                  rows={8}
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
            form="setting-form"
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
