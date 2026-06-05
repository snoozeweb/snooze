import { useEffect, useState } from "react";
import { useForm } from "react-hook-form";
import { Button } from "@/shared/ui/Button";
import { Drawer, DrawerBody, DrawerContent, DrawerFooter, DrawerTitle } from "@/shared/ui/Drawer";
import { Input } from "@/shared/ui/Input";
import { Select, SelectContent, SelectItem, SelectTrigger } from "@/shared/ui/Select";
import { Spinner } from "@/shared/ui/Spinner";
import { toast } from "@/shared/ui/toast/useToast";
import { ApiError } from "@/lib/api/client";
import { Tenants } from "./api";
import type { Tenant } from "./types";
import styles from "./TenantEditor.module.css";

// The reserved slug that must never be deleted. Mirrors snoozetypes.DefaultTenant.
const DEFAULT_TENANT = "default";

type FormShape = {
  id: string;
  display_name: string;
  status: "active" | "suspended";
};

const EMPTY_FORM: FormShape = {
  id: "",
  display_name: "",
  status: "active",
};

export type TenantEditorProps = {
  /** undefined = create mode; a slug string = edit mode. */
  id: string | undefined;
  onClose: () => void;
};

export function TenantEditor({ id, onClose }: TenantEditorProps) {
  const isCreate = id === undefined || id === "";
  const existing = Tenants.useGet(isCreate ? undefined : id);
  const create = Tenants.useCreate();
  const update = Tenants.useUpdate();
  const remove = Tenants.useRemove();

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
        id: existing.data.id,
        display_name: existing.data.display_name,
        status: existing.data.status === "suspended" ? "suspended" : "active",
      });
    }
  }, [existing.data, isCreate, reset]);

  const [submitting, setSubmitting] = useState(false);
  const [deleting, setDeleting] = useState(false);

  const currentId = watch("id");
  const isDefault = !isCreate && id === DEFAULT_TENANT;

  async function onSubmit(form: FormShape) {
    setSubmitting(true);
    try {
      if (isCreate) {
        const body: Omit<Tenant, "created_at" | "updated_at"> = {
          id: form.id.trim(),
          display_name: form.display_name.trim(),
          status: form.status,
        };
        await create.mutateAsync(body);
        toast.success("Tenant created");
      } else {
        await update.mutateAsync({
          uid: id,
          body: {
            display_name: form.display_name.trim(),
            status: form.status,
          },
        });
        toast.success("Tenant saved");
      }
      onClose();
    } catch (e) {
      toast.error(e instanceof ApiError ? e.detail : "Save failed");
    } finally {
      setSubmitting(false);
    }
  }

  async function handleDelete() {
    if (isDefault) return;
    setDeleting(true);
    try {
      await remove.mutateAsync(id!);
      toast.success("Tenant deleted");
      onClose();
    } catch (e) {
      toast.error(e instanceof ApiError ? e.detail : "Delete failed");
    } finally {
      setDeleting(false);
    }
  }

  const idInvalid = formState.isSubmitted && !currentId.trim();
  const displayNameInvalid = formState.isSubmitted && !watch("display_name").trim();

  return (
    <Drawer
      open
      onOpenChange={(o) => {
        if (!o) onClose();
      }}
    >
      <DrawerContent>
        <DrawerTitle>{isCreate ? "New tenant" : "Edit tenant"}</DrawerTitle>
        <DrawerBody>
          {!isCreate && existing.isPending ? (
            <div style={{ display: "flex", justifyContent: "center", padding: "var(--space-5)" }}>
              <Spinner size={20} />
            </div>
          ) : (
            <form
              id="tenant-form"
              className={styles.stack}
              onSubmit={(e) => void handleSubmit(onSubmit)(e)}
            >
              <section className={styles.section}>
                <h3 className={styles.sectionTitle}>Identity</h3>
                <div className={styles.field}>
                  <label className={styles.label} htmlFor="tenant-id">
                    Slug
                  </label>
                  <Input
                    id="tenant-id"
                    {...register("id")}
                    invalid={idInvalid}
                    placeholder="e.g. acme"
                    disabled={!isCreate}
                  />
                </div>
                <div className={styles.field}>
                  <label className={styles.label} htmlFor="tenant-display-name">
                    Display Name
                  </label>
                  <Input
                    id="tenant-display-name"
                    {...register("display_name")}
                    invalid={displayNameInvalid}
                    placeholder="e.g. Acme Corp"
                  />
                </div>
                <div className={styles.field}>
                  <span className={styles.label} id="tenant-status-label">
                    Status
                  </span>
                  <Select
                    value={watch("status")}
                    onValueChange={(v) =>
                      setValue("status", v as "active" | "suspended", { shouldDirty: true })
                    }
                  >
                    <SelectTrigger />
                    <SelectContent>
                      <SelectItem value="active">Active</SelectItem>
                      <SelectItem value="suspended">Suspended</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
              </section>
              {!isCreate ? (
                <div className={styles.dangerZone}>
                  <Button
                    type="button"
                    variant="danger"
                    leadingIcon="trash"
                    size="sm"
                    onClick={() => void handleDelete()}
                    loading={deleting}
                    disabled={isDefault || deleting}
                  >
                    Delete tenant
                  </Button>
                </div>
              ) : null}
            </form>
          )}
        </DrawerBody>
        <DrawerFooter>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button
            type="submit"
            form="tenant-form"
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
