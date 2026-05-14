import { useEffect, useState } from "react";
import { useForm } from "react-hook-form";
import { Button } from "@/shared/ui/Button";
import { Drawer, DrawerBody, DrawerContent, DrawerFooter, DrawerTitle } from "@/shared/ui/Drawer";
import { Input } from "@/shared/ui/Input";
import { Spinner } from "@/shared/ui/Spinner";
import { Textarea } from "@/shared/ui/Textarea";
import { toast } from "@/shared/ui/toast/useToast";
import { ApiError } from "@/lib/api/client";
import { Roles } from "./api";
import type { Role } from "./types";
import styles from "./RoleEditor.module.css";

type FormShape = {
  name: string;
  permissionsText: string;
  comment: string;
};

const EMPTY_FORM: FormShape = {
  name: "",
  permissionsText: "",
  comment: "",
};

export type RoleEditorProps = {
  uid: string | undefined;
  onClose: () => void;
};

export function RoleEditor({ uid, onClose }: RoleEditorProps) {
  const isCreate = uid === undefined || uid === "";
  const existing = Roles.useGet(isCreate ? undefined : uid);
  const create = Roles.useCreate();
  const update = Roles.useUpdate();

  const { register, handleSubmit, reset, watch, formState } = useForm<FormShape>({
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
        permissionsText: (existing.data.permissions ?? []).join("\n"),
        comment: existing.data.comment ?? "",
      });
    }
  }, [existing.data, isCreate, reset]);

  const [submitting, setSubmitting] = useState(false);

  async function onSubmit(form: FormShape) {
    setSubmitting(true);
    try {
      const permissions = form.permissionsText
        .split("\n")
        .map((p) => p.trim())
        .filter((p) => p.length > 0);

      const body: Role = {
        name: form.name,
        permissions,
        ...(form.comment ? { comment: form.comment } : {}),
      };
      if (isCreate) {
        await create.mutateAsync(body);
        toast.success("Role created");
      } else {
        await update.mutateAsync({ uid, body });
        toast.success("Role saved");
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
        <DrawerTitle>{isCreate ? "New role" : "Edit role"}</DrawerTitle>
        <DrawerBody>
          {!isCreate && existing.isPending ? (
            <div style={{ display: "flex", justifyContent: "center", padding: "var(--space-5)" }}>
              <Spinner size={20} />
            </div>
          ) : (
            <form
              id="role-form"
              className={styles.stack}
              onSubmit={(e) => void handleSubmit(onSubmit)(e)}
            >
              <section className={styles.section}>
                <h3 className={styles.sectionTitle}>Identity</h3>
                <div className={styles.field}>
                  <label className={styles.label} htmlFor="role-name">
                    Name
                  </label>
                  <Input
                    id="role-name"
                    {...register("name")}
                    invalid={nameInvalid}
                    placeholder="e.g. analyst"
                  />
                </div>
                <div className={styles.field}>
                  <label className={styles.label} htmlFor="role-permissions">
                    Permissions
                  </label>
                  <Textarea
                    id="role-permissions"
                    {...register("permissionsText")}
                    rows={6}
                    style={{ fontFamily: "monospace" }}
                    placeholder="one per line, e.g. rw_rule"
                  />
                </div>
                <div className={styles.field}>
                  <label className={styles.label} htmlFor="role-comment">
                    Comment
                  </label>
                  <Textarea
                    id="role-comment"
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
            form="role-form"
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
