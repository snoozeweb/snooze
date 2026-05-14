import { useEffect, useState } from "react";
import { useForm } from "react-hook-form";
import { Button } from "@/shared/ui/Button";
import { Drawer, DrawerBody, DrawerContent, DrawerFooter, DrawerTitle } from "@/shared/ui/Drawer";
import { Input } from "@/shared/ui/Input";
import { Spinner } from "@/shared/ui/Spinner";
import { Textarea } from "@/shared/ui/Textarea";
import { toast } from "@/shared/ui/toast/useToast";
import { ApiError } from "@/lib/api/client";
import { KVs } from "./api";
import type { KV } from "./types";
import styles from "./KVEditor.module.css";

type FormShape = {
  key: string;
  value: string;
  comment: string;
};

const EMPTY_FORM: FormShape = {
  key: "",
  value: "",
  comment: "",
};

export type KVEditorProps = {
  uid: string | undefined;
  onClose: () => void;
};

export function KVEditor({ uid, onClose }: KVEditorProps) {
  const isCreate = uid === undefined || uid === "";
  const existing = KVs.useGet(isCreate ? undefined : uid);
  const create = KVs.useCreate();
  const update = KVs.useUpdate();

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
        key: existing.data.key ?? "",
        value: existing.data.value ?? "",
        comment: existing.data.comment ?? "",
      });
    }
  }, [existing.data, isCreate, reset]);

  const [submitting, setSubmitting] = useState(false);

  async function onSubmit(form: FormShape) {
    setSubmitting(true);
    try {
      const body: KV = {
        key: form.key,
        ...(form.value ? { value: form.value } : {}),
        ...(form.comment ? { comment: form.comment } : {}),
      };
      if (isCreate) {
        await create.mutateAsync(body);
        toast.success("Key-value created");
      } else {
        await update.mutateAsync({ uid, body });
        toast.success("Key-value saved");
      }
      onClose();
    } catch (e) {
      toast.error(e instanceof ApiError ? e.detail : "Save failed");
    } finally {
      setSubmitting(false);
    }
  }

  const keyInvalid = formState.isSubmitted && !watch("key").trim();

  return (
    <Drawer
      open
      onOpenChange={(o) => {
        if (!o) onClose();
      }}
    >
      <DrawerContent>
        <DrawerTitle>{isCreate ? "New key-value" : "Edit key-value"}</DrawerTitle>
        <DrawerBody>
          {!isCreate && existing.isPending ? (
            <div style={{ display: "flex", justifyContent: "center", padding: "var(--space-5)" }}>
              <Spinner size={20} />
            </div>
          ) : (
            <form
              id="kv-form"
              className={styles.stack}
              onSubmit={(e) => void handleSubmit(onSubmit)(e)}
            >
              <section className={styles.section}>
                <h3 className={styles.sectionTitle}>Identity</h3>
                <div className={styles.field}>
                  <label className={styles.label} htmlFor="kv-key">
                    Key
                  </label>
                  <Input
                    id="kv-key"
                    {...register("key")}
                    invalid={keyInvalid}
                    placeholder="e.g. MY_SETTING"
                  />
                </div>
                <div className={styles.field}>
                  <label className={styles.label} htmlFor="kv-value">
                    Value
                  </label>
                  <Textarea
                    id="kv-value"
                    {...register("value")}
                    rows={4}
                    placeholder="Optional value"
                  />
                </div>
                <div className={styles.field}>
                  <label className={styles.label} htmlFor="kv-comment">
                    Comment
                  </label>
                  <Textarea
                    id="kv-comment"
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
            form="kv-form"
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
