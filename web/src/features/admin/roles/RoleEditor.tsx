import { useEffect, useMemo, useState } from "react";
import { useForm } from "react-hook-form";
import { Button } from "@/shared/ui/Button";
import { Drawer, DrawerBody, DrawerContent, DrawerFooter, DrawerTitle } from "@/shared/ui/Drawer";
import { Input } from "@/shared/ui/Input";
import { MultiCombobox } from "@/shared/ui/MultiCombobox";
import { Spinner } from "@/shared/ui/Spinner";
import { Textarea } from "@/shared/ui/Textarea";
import { toast } from "@/shared/ui/toast/useToast";
import { ApiError } from "@/lib/api/client";
import { Roles, usePermissionsCatalogue } from "./api";
import type { Role } from "./types";
import styles from "./RoleEditor.module.css";

type FormShape = {
  name: string;
  permissions: string[];
  comment: string;
};

const EMPTY_FORM: FormShape = {
  name: "",
  permissions: [],
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
  const catalogue = usePermissionsCatalogue();

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
        permissions: existing.data.permissions ?? [],
        comment: existing.data.comment ?? "",
      });
    }
  }, [existing.data, isCreate, reset]);

  const [submitting, setSubmitting] = useState(false);

  async function onSubmit(form: FormShape) {
    setSubmitting(true);
    try {
      const body: Role = {
        name: form.name,
        permissions: form.permissions,
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
  const permissions = watch("permissions");

  // Merge the catalogue with any permissions already on the role so a
  // legacy/unknown value still renders as a badge and survives a Save
  // round-trip. Mirrors the pattern used by UserEditor for roles.
  const permissionOptions = useMemo(() => {
    const known = catalogue.data ?? [];
    const seen = new Set(known);
    const merged = known.map((p) => ({ value: p, label: p }));
    for (const p of permissions) {
      if (!seen.has(p)) {
        merged.push({ value: p, label: p });
        seen.add(p);
      }
    }
    return merged;
  }, [catalogue.data, permissions]);

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
                  {/* MultiCombobox is not a native form control, so the label
                      is associated by aria-label rather than htmlFor — the
                      visible <label> is purely cosmetic. */}
                  <span className={styles.label} id="role-permissions-label">
                    Permissions
                  </span>
                  <MultiCombobox
                    aria-label="Permissions"
                    placeholder="Select one or more permissions"
                    options={permissionOptions}
                    value={permissions}
                    onChange={(next) => setValue("permissions", next, { shouldDirty: true })}
                  />
                </div>
              </section>
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
