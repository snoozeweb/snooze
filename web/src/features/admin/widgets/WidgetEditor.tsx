import { useEffect, useState } from "react";
import { useForm } from "react-hook-form";
import { Button } from "@/shared/ui/Button";
import { Drawer, DrawerBody, DrawerContent, DrawerFooter, DrawerTitle } from "@/shared/ui/Drawer";
import { Input } from "@/shared/ui/Input";
import { Spinner } from "@/shared/ui/Spinner";
import { Textarea } from "@/shared/ui/Textarea";
import { toast } from "@/shared/ui/toast/useToast";
import { ApiError } from "@/lib/api/client";
import { Widgets } from "./api";
import type { Widget } from "./types";
import styles from "./WidgetEditor.module.css";

type FormShape = {
  name: string;
  widget_type: string;
  config_json: string;
  comment: string;
};

const EMPTY_FORM: FormShape = {
  name: "",
  widget_type: "",
  config_json: "{}",
  comment: "",
};

export type WidgetEditorProps = {
  uid: string | undefined;
  onClose: () => void;
};

export function WidgetEditor({ uid, onClose }: WidgetEditorProps) {
  const isCreate = uid === undefined || uid === "";
  const existing = Widgets.useGet(isCreate ? undefined : uid);
  const create = Widgets.useCreate();
  const update = Widgets.useUpdate();

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
        widget_type: existing.data.widget_type ?? "",
        config_json: JSON.stringify(existing.data.config ?? {}, null, 2),
        comment: existing.data.comment ?? "",
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
        parsed = JSON.parse(form.config_json) as Record<string, unknown>;
        if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) {
          throw new Error("expected a JSON object");
        }
      } catch (e) {
        setJsonError(e instanceof Error ? e.message : "Invalid JSON");
        setSubmitting(false);
        return;
      }
      const body: Widget = {
        name: form.name,
        ...(form.widget_type ? { widget_type: form.widget_type } : {}),
        config: parsed,
        ...(form.comment ? { comment: form.comment } : {}),
      };
      if (isCreate) {
        await create.mutateAsync(body);
        toast.success("Widget created");
      } else {
        await update.mutateAsync({ uid, body });
        toast.success("Widget saved");
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
        <DrawerTitle>{isCreate ? "New widget" : "Edit widget"}</DrawerTitle>
        <DrawerBody>
          {!isCreate && existing.isPending ? (
            <div style={{ display: "flex", justifyContent: "center", padding: "var(--space-5)" }}>
              <Spinner size={20} />
            </div>
          ) : (
            <form
              id="widget-form"
              className={styles.stack}
              onSubmit={(e) => void handleSubmit(onSubmit)(e)}
            >
              <section className={styles.section}>
                <h3 className={styles.sectionTitle}>Identity</h3>
                <div className={styles.field}>
                  <label className={styles.label} htmlFor="widget-name">
                    Name
                  </label>
                  <Input
                    id="widget-name"
                    {...register("name")}
                    invalid={nameInvalid}
                    placeholder="e.g. patlite-floor1"
                  />
                </div>
                <div className={styles.field}>
                  <label className={styles.label} htmlFor="widget-type">
                    Widget type
                  </label>
                  <Input
                    id="widget-type"
                    {...register("widget_type")}
                    placeholder="patlite | iframe | …"
                  />
                </div>
                <div className={styles.field}>
                  <label className={styles.label} htmlFor="widget-comment">
                    Comment
                  </label>
                  <Textarea id="widget-comment" {...register("comment")} rows={2} />
                </div>
              </section>
              <section className={styles.section}>
                <h3 className={styles.sectionTitle}>Config (JSON)</h3>
                <Textarea
                  {...register("config_json")}
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
            form="widget-form"
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
