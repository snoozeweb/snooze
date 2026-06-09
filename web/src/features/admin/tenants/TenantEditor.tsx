import { useEffect, useState } from "react";
import { useForm } from "react-hook-form";
import { Button } from "@/shared/ui/Button";
import { Checkbox } from "@/shared/ui/Checkbox";
import { Dialog, DialogContent, DialogTitle, DialogBody, DialogFooter } from "@/shared/ui/Dialog";
import { Drawer, DrawerBody, DrawerContent, DrawerFooter, DrawerTitle } from "@/shared/ui/Drawer";
import { Input } from "@/shared/ui/Input";
import { Select, SelectContent, SelectItem, SelectTrigger } from "@/shared/ui/Select";
import { Spinner } from "@/shared/ui/Spinner";
import { toast } from "@/shared/ui/toast/useToast";
import { ApiError } from "@/lib/api/client";
import { Tenants } from "./api";
import { AdminCredentialDialog } from "./AdminCredentialDialog";
import type { AdminCredential, CreateTenantBody } from "./types";
import styles from "./TenantEditor.module.css";

// The reserved slug that must never be deleted. Mirrors snoozetypes.DefaultTenant.
const DEFAULT_TENANT = "default";

type FormShape = {
  id: string;
  display_name: string;
  status: "active" | "suspended";
  create_admin: boolean;
  admin_username: string;
  listed: boolean;
};

const EMPTY_FORM: FormShape = {
  id: "",
  display_name: "",
  status: "active",
  create_admin: true,
  admin_username: "admin",
  listed: true,
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
  const rotateLoginKey = Tenants.useRotateLoginKey();

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
        listed: existing.data.listed !== false,
      });
    }
  }, [existing.data, isCreate, reset]);

  const [submitting, setSubmitting] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [revealed, setRevealed] = useState<AdminCredential | null>(null);
  const [confirmRotate, setConfirmRotate] = useState(false);
  const [rotating, setRotating] = useState(false);

  const currentId = watch("id");
  const isDefault = !isCreate && id === DEFAULT_TENANT;

  async function onSubmit(form: FormShape) {
    setSubmitting(true);
    try {
      if (isCreate) {
        const body: CreateTenantBody = {
          id: form.id.trim(),
          display_name: form.display_name.trim(),
          status: form.status,
          create_admin: form.create_admin,
          ...(form.create_admin && form.admin_username.trim()
            ? { admin_username: form.admin_username.trim() }
            : {}),
        };
        const res = await create.mutateAsync(body);
        toast.success("Tenant created");
        if (res.admin) {
          setRevealed(res.admin);
          return;
        }
        onClose();
        return;
      } else {
        await update.mutateAsync({
          uid: id,
          body: {
            display_name: form.display_name.trim(),
            status: form.status,
            listed: form.listed,
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

  async function handleRotateConfirmed() {
    if (!id) return;
    setRotating(true);
    try {
      await rotateLoginKey.mutateAsync(id);
      setConfirmRotate(false);
      toast.success("Login key rotated");
    } catch (e) {
      toast.error(e instanceof ApiError ? e.detail : "Rotate failed");
    } finally {
      setRotating(false);
    }
  }

  const loginKey = existing.data?.login_key;
  const loginLink = loginKey ? `${window.location.origin}/web/login?key=${loginKey}` : null;

  const idInvalid = formState.isSubmitted && !currentId.trim();
  const displayNameInvalid = formState.isSubmitted && !watch("display_name").trim();

  return (
    <>
      <AdminCredentialDialog
        credential={revealed}
        onClose={() => {
          setRevealed(null);
          onClose();
        }}
      />
      <Dialog
        open={confirmRotate}
        onOpenChange={(o) => {
          if (!o) setConfirmRotate(false);
        }}
      >
        <DialogContent>
          <DialogTitle>Rotate login key?</DialogTitle>
          <DialogBody>
            This generates a new login link for {existing.data?.display_name || id}. The previous
            link will stop working immediately.
          </DialogBody>
          <DialogFooter>
            <Button variant="secondary" onClick={() => setConfirmRotate(false)} disabled={rotating}>
              Cancel
            </Button>
            <Button
              variant="primary"
              disabled={rotating}
              onClick={() => void handleRotateConfirmed()}
            >
              {rotating ? "Rotating…" : "Rotate login key"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
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
                  <div
                    className={styles.field}
                    style={{ flexDirection: "row", alignItems: "center", gap: "var(--space-2)" }}
                  >
                    <Checkbox
                      id="tenant-listed"
                      checked={watch("listed")}
                      onCheckedChange={(v) => setValue("listed", v === true, { shouldDirty: true })}
                      aria-label="Listed"
                    />
                    <label className={styles.label} htmlFor="tenant-listed">
                      Listed
                    </label>
                  </div>
                </section>
                {!isCreate ? (
                  <section className={styles.section}>
                    <h3 className={styles.sectionTitle}>Login link</h3>
                    {loginLink ? (
                      <>
                        <div className={styles.field}>
                          <span className={styles.label}>Shareable link</span>
                          <div
                            style={{ display: "flex", gap: "var(--space-2)", alignItems: "center" }}
                          >
                            <Input
                              readOnly
                              value={loginLink}
                              aria-label="Login link"
                              style={{
                                flex: 1,
                                fontFamily: "monospace",
                                fontSize: "var(--font-size-sm)",
                              }}
                            />
                            <Button
                              type="button"
                              size="sm"
                              variant="ghost"
                              onClick={() => void navigator.clipboard?.writeText(loginLink)}
                            >
                              Copy
                            </Button>
                          </div>
                        </div>
                        <div>
                          <Button
                            type="button"
                            size="sm"
                            variant="secondary"
                            onClick={() => setConfirmRotate(true)}
                            disabled={rotating}
                          >
                            Rotate
                          </Button>
                        </div>
                      </>
                    ) : (
                      <div>
                        <Button
                          type="button"
                          size="sm"
                          variant="secondary"
                          onClick={() => setConfirmRotate(true)}
                          disabled={rotating}
                        >
                          Generate login link
                        </Button>
                      </div>
                    )}
                  </section>
                ) : null}
                {isCreate ? (
                  <section className={styles.section}>
                    <h3 className={styles.sectionTitle}>Admin provisioning</h3>
                    <div className={styles.field}>
                      <Checkbox
                        id="tenant-create-admin"
                        checked={watch("create_admin")}
                        onCheckedChange={(v) =>
                          setValue("create_admin", v === true, { shouldDirty: true })
                        }
                        aria-label="Create admin user"
                      />
                      <label className={styles.label} htmlFor="tenant-create-admin">
                        Create admin user
                      </label>
                    </div>
                    {watch("create_admin") ? (
                      <div className={styles.field}>
                        <label className={styles.label} htmlFor="tenant-admin-username">
                          Admin username
                        </label>
                        <Input
                          id="tenant-admin-username"
                          {...register("admin_username")}
                          aria-label="Admin username"
                          placeholder="admin"
                        />
                      </div>
                    ) : null}
                  </section>
                ) : null}
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
    </>
  );
}
