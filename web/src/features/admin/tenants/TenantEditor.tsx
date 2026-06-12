import { useState, type ReactNode } from "react";
import {
  useFormState,
  useWatch,
  type Control,
  type UseFormSetValue,
  type UseFormRegister,
} from "react-hook-form";
import { Button } from "@/shared/ui/Button";
import { Checkbox } from "@/shared/ui/Checkbox";
import { Dialog, DialogContent, DialogTitle, DialogBody, DialogFooter } from "@/shared/ui/Dialog";
import { Input } from "@/shared/ui/Input";
import { Select, SelectContent, SelectItem, SelectTrigger } from "@/shared/ui/Select";
import { toast } from "@/shared/ui/toast/useToast";
import { ApiError } from "@/lib/api/client";
import { EditorDrawer, EditorAbort, type EditorBodyProps } from "@/shared/forms/EditorDrawer";
import { Tenants, type TenantUpdateBody } from "./api";
import { AdminCredentialDialog } from "./AdminCredentialDialog";
import type { AdminCredential, CreateTenantBody, CreateTenantResult, Tenant } from "./types";
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
  const get = Tenants.useGet(isCreate ? undefined : id);
  const create = Tenants.useCreate();
  const update = Tenants.useUpdate();
  const remove = Tenants.useRemove();
  const rotateLoginKey = Tenants.useRotateLoginKey();

  const [deleting, setDeleting] = useState(false);
  const [revealed, setRevealed] = useState<AdminCredential | null>(null);
  const [confirmRotate, setConfirmRotate] = useState(false);
  const [rotating, setRotating] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);

  const isDefault = !isCreate && id === DEFAULT_TENANT;

  async function handleDeleteConfirmed() {
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
      setConfirmDelete(false);
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

  const loginKey = get.data?.login_key;
  const loginLink = loginKey ? `${window.location.origin}/web/login?key=${loginKey}` : null;

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
            This generates a new login link for {get.data?.display_name || id}. The previous link
            will stop working immediately.
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
      <Dialog
        open={confirmDelete}
        onOpenChange={(o) => {
          if (!o) setConfirmDelete(false);
        }}
      >
        <DialogContent>
          <DialogTitle>Delete tenant?</DialogTitle>
          <DialogBody>
            Delete tenant <strong>{get.data?.display_name || id}</strong>? All its data becomes
            inaccessible. This action cannot be undone.
          </DialogBody>
          <DialogFooter>
            <Button variant="secondary" onClick={() => setConfirmDelete(false)} disabled={deleting}>
              Cancel
            </Button>
            <Button
              variant="danger"
              disabled={deleting}
              onClick={() => void handleDeleteConfirmed()}
            >
              {deleting ? "Deleting…" : "Delete tenant"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      <EditorDrawer<FormShape, Tenant, CreateTenantBody, TenantUpdateBody, CreateTenantResult>
        uid={id}
        onClose={onClose}
        get={get}
        create={create}
        update={update}
        emptyForm={EMPTY_FORM}
        recordToForm={(t) => ({
          id: t.id,
          display_name: t.display_name,
          status: t.status === "suspended" ? "suspended" : "active",
          create_admin: true,
          admin_username: "admin",
          listed: t.listed !== false,
        })}
        formToBody={(form, create): CreateTenantBody | TenantUpdateBody => {
          if (create) {
            if (!form.id.trim() || !form.display_name.trim()) throw new EditorAbort();
            return {
              id: form.id.trim(),
              display_name: form.display_name.trim(),
              status: form.status,
              create_admin: form.create_admin,
              ...(form.create_admin && form.admin_username.trim()
                ? { admin_username: form.admin_username.trim() }
                : {}),
            };
          }
          if (!form.display_name.trim()) throw new EditorAbort();
          return {
            display_name: form.display_name.trim(),
            status: form.status,
            listed: form.listed,
          };
        }}
        title={(c): ReactNode => (c ? "New tenant" : "Edit tenant")}
        onCreated={(res) => {
          // A fresh tenant's one-time admin credential is revealed in a dialog
          // instead of closing the drawer. Returning false suppresses the
          // frame's auto-close; the credential dialog's onClose closes us.
          if (res.admin) {
            setRevealed(res.admin);
            return false;
          }
        }}
        successMessage={{ create: "Tenant created", update: "Tenant saved" }}
        formId="tenant-form"
        formClassName={styles.stack}
      >
        {(body) => (
          <TenantFields
            {...body}
            isDefault={isDefault}
            deleting={deleting}
            rotating={rotating}
            loginLink={loginLink}
            onAskDelete={() => setConfirmDelete(true)}
            onAskRotate={() => setConfirmRotate(true)}
          />
        )}
      </EditorDrawer>
    </>
  );
}

function TenantFields({
  control,
  register,
  setValue,
  isCreate,
  isDefault,
  deleting,
  rotating,
  loginLink,
  onAskDelete,
  onAskRotate,
}: EditorBodyProps<FormShape> & {
  isDefault: boolean;
  deleting: boolean;
  rotating: boolean;
  loginLink: string | null;
  onAskDelete: () => void;
  onAskRotate: () => void;
}) {
  return (
    <>
      <section className={styles.section}>
        <h3 className={styles.sectionTitle}>Identity</h3>
        <div className={styles.field}>
          <label className={styles.label} htmlFor="tenant-id">
            Slug
          </label>
          <TenantIdField control={control} register={register} isCreate={isCreate} />
        </div>
        <div className={styles.field}>
          <label className={styles.label} htmlFor="tenant-display-name">
            Display Name
          </label>
          <TenantDisplayNameField control={control} register={register} />
        </div>
        <div className={styles.field}>
          <span className={styles.label} id="tenant-status-label">
            Status
          </span>
          <TenantStatusSelect control={control} setValue={setValue} />
        </div>
        <div
          className={styles.field}
          style={{ flexDirection: "row", alignItems: "center", gap: "var(--space-2)" }}
        >
          <TenantListedCheckbox control={control} setValue={setValue} />
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
                <div style={{ display: "flex", gap: "var(--space-2)", alignItems: "center" }}>
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
                  onClick={onAskRotate}
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
                onClick={onAskRotate}
                disabled={rotating}
              >
                Generate login link
              </Button>
            </div>
          )}
        </section>
      ) : null}
      {isCreate ? (
        <TenantAdminProvisioning control={control} register={register} setValue={setValue} />
      ) : null}
      {!isCreate ? (
        <div className={styles.dangerZone}>
          <Button
            type="button"
            variant="danger"
            leadingIcon="trash"
            size="sm"
            onClick={onAskDelete}
            loading={deleting}
            disabled={isDefault || deleting}
          >
            Delete tenant
          </Button>
        </div>
      ) : null}
    </>
  );
}

function TenantIdField({
  control,
  register,
  isCreate,
}: {
  control: Control<FormShape>;
  register: UseFormRegister<FormShape>;
  isCreate: boolean;
}) {
  const id = useWatch({ control, name: "id" });
  const { isSubmitted } = useFormState({ control });
  const invalid = isSubmitted && !id.trim();
  return (
    <Input
      id="tenant-id"
      {...register("id")}
      invalid={invalid}
      placeholder="e.g. acme"
      disabled={!isCreate}
    />
  );
}

function TenantDisplayNameField({
  control,
  register,
}: {
  control: Control<FormShape>;
  register: UseFormRegister<FormShape>;
}) {
  const value = useWatch({ control, name: "display_name" });
  const { isSubmitted } = useFormState({ control });
  const invalid = isSubmitted && !value.trim();
  return (
    <Input
      id="tenant-display-name"
      {...register("display_name")}
      invalid={invalid}
      placeholder="e.g. Acme Corp"
    />
  );
}

function TenantStatusSelect({
  control,
  setValue,
}: {
  control: Control<FormShape>;
  setValue: UseFormSetValue<FormShape>;
}) {
  const status = useWatch({ control, name: "status" });
  return (
    <Select
      value={status}
      onValueChange={(v) => setValue("status", v as "active" | "suspended", { shouldDirty: true })}
    >
      <SelectTrigger aria-labelledby="tenant-status-label" />
      <SelectContent>
        <SelectItem value="active">Active</SelectItem>
        <SelectItem value="suspended">Suspended</SelectItem>
      </SelectContent>
    </Select>
  );
}

function TenantListedCheckbox({
  control,
  setValue,
}: {
  control: Control<FormShape>;
  setValue: UseFormSetValue<FormShape>;
}) {
  const listed = useWatch({ control, name: "listed" });
  return (
    <Checkbox
      id="tenant-listed"
      checked={listed}
      onCheckedChange={(v) => setValue("listed", v === true, { shouldDirty: true })}
      aria-label="Listed"
    />
  );
}

function TenantAdminProvisioning({
  control,
  register,
  setValue,
}: {
  control: Control<FormShape>;
  register: UseFormRegister<FormShape>;
  setValue: UseFormSetValue<FormShape>;
}) {
  const createAdmin = useWatch({ control, name: "create_admin" });
  return (
    <section className={styles.section}>
      <h3 className={styles.sectionTitle}>Admin provisioning</h3>
      <div className={styles.field}>
        <Checkbox
          id="tenant-create-admin"
          checked={createAdmin}
          onCheckedChange={(v) => setValue("create_admin", v === true, { shouldDirty: true })}
          aria-label="Create admin user"
        />
        <label className={styles.label} htmlFor="tenant-create-admin">
          Create admin user
        </label>
      </div>
      {createAdmin ? (
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
  );
}
