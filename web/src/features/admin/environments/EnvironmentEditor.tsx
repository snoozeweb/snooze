import { useEffect, useState } from "react";
import { useForm } from "react-hook-form";
import { Button } from "@/shared/ui/Button";
import { Drawer, DrawerBody, DrawerContent, DrawerFooter, DrawerTitle } from "@/shared/ui/Drawer";
import { Input } from "@/shared/ui/Input";
import { Spinner } from "@/shared/ui/Spinner";
import { Textarea } from "@/shared/ui/Textarea";
import { toast } from "@/shared/ui/toast/useToast";
import { ApiError } from "@/lib/api/client";
import { Environments } from "./api";
import type { Environment } from "./types";
import styles from "./EnvironmentEditor.module.css";

type FormShape = {
  name: string;
  color: string;
  comment: string;
};

const EMPTY_FORM: FormShape = {
  name: "",
  color: "#000000",
  comment: "",
};

export type EnvironmentEditorProps = {
  uid: string | undefined;
  onClose: () => void;
};

export function EnvironmentEditor({ uid, onClose }: EnvironmentEditorProps) {
  const isCreate = uid === undefined || uid === "";
  const existing = Environments.useGet(isCreate ? undefined : uid);
  const create = Environments.useCreate();
  const update = Environments.useUpdate();

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
        color: existing.data.color ?? "#000000",
        comment: existing.data.comment ?? "",
      });
    }
  }, [existing.data, isCreate, reset]);

  const [submitting, setSubmitting] = useState(false);

  async function onSubmit(form: FormShape) {
    setSubmitting(true);
    try {
      const body: Environment = {
        name: form.name,
        ...(form.color ? { color: form.color } : {}),
        ...(form.comment ? { comment: form.comment } : {}),
      };
      if (isCreate) {
        await create.mutateAsync(body);
        toast.success("Environment created");
      } else {
        await update.mutateAsync({ uid, body });
        toast.success("Environment saved");
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
        <DrawerTitle>{isCreate ? "New environment" : "Edit environment"}</DrawerTitle>
        <DrawerBody>
          {!isCreate && existing.isPending ? (
            <div style={{ display: "flex", justifyContent: "center", padding: "var(--space-5)" }}>
              <Spinner size={20} />
            </div>
          ) : (
            <form
              id="environment-form"
              className={styles.stack}
              onSubmit={(e) => void handleSubmit(onSubmit)(e)}
            >
              <section className={styles.section}>
                <h3 className={styles.sectionTitle}>Identity</h3>
                <div className={styles.field}>
                  <label className={styles.label} htmlFor="environment-name">
                    Name
                  </label>
                  <Input
                    id="environment-name"
                    {...register("name")}
                    invalid={nameInvalid}
                    placeholder="e.g. production"
                  />
                </div>
                <div className={styles.field}>
                  <label className={styles.label} htmlFor="environment-color">
                    Color
                  </label>
                  <input id="environment-color" type="color" {...register("color")} />
                </div>
                <div className={styles.field}>
                  <label className={styles.label} htmlFor="environment-comment">
                    Comment
                  </label>
                  <Textarea
                    id="environment-comment"
                    {...register("comment")}
                    rows={2}
                    placeholder="Optional description"
                  />
                </div>
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
            form="environment-form"
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
