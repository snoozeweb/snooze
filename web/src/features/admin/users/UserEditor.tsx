import { useEffect, useMemo, useState } from "react";
import { useForm } from "react-hook-form";
import { Button } from "@/shared/ui/Button";
import { Drawer, DrawerBody, DrawerContent, DrawerFooter, DrawerTitle } from "@/shared/ui/Drawer";
import { Input } from "@/shared/ui/Input";
import { MultiCombobox } from "@/shared/ui/MultiCombobox";
import { Spinner } from "@/shared/ui/Spinner";
import { Textarea } from "@/shared/ui/Textarea";
import { toast } from "@/shared/ui/toast/useToast";
import { Select, SelectContent, SelectItem, SelectTrigger } from "@/shared/ui/Select";
import { ApiError } from "@/lib/api/client";
import { Roles } from "@/features/admin/roles/api";
import { Users } from "./api";
import type { User } from "./types";
import styles from "./UserEditor.module.css";

type FormShape = {
  name: string;
  type: "local" | "ldap";
  roles: string[];
  comment: string;
  password: string;
};

const EMPTY_FORM: FormShape = {
  name: "",
  type: "local",
  roles: [],
  comment: "",
  password: "",
};

export type UserEditorProps = {
  uid: string | undefined;
  onClose: () => void;
};

export function UserEditor({ uid, onClose }: UserEditorProps) {
  const isCreate = uid === undefined || uid === "";
  const existing = Users.useGet(isCreate ? undefined : uid);
  const create = Users.useCreate();
  const update = Users.useUpdate();

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
        type: existing.data.type ?? "local",
        roles: existing.data.roles ?? [],
        comment: existing.data.comment ?? "",
        password: "",
      });
    }
  }, [existing.data, isCreate, reset]);

  const [submitting, setSubmitting] = useState(false);

  async function onSubmit(form: FormShape) {
    setSubmitting(true);
    try {
      const body: User = {
        name: form.name,
        type: form.type,
        ...(form.roles.length ? { roles: form.roles } : {}),
        ...(form.comment ? { comment: form.comment } : {}),
        ...(isCreate && form.password ? { password: form.password } : {}),
      };

      if (isCreate) {
        await create.mutateAsync(body);
        toast.success("User created");
      } else {
        await update.mutateAsync({ uid, body });
        toast.success("User saved");
      }
      onClose();
    } catch (e) {
      toast.error(e instanceof ApiError ? e.detail : "Save failed");
    } finally {
      setSubmitting(false);
    }
  }

  const typeValue = watch("type");
  const rolesValue = watch("roles");
  const nameInvalid = formState.isSubmitted && !watch("name").trim();

  const rolesList = Roles.useList({ limit: 500 });
  const roleOptions = useMemo(() => {
    const available = (rolesList.data?.data ?? []).map((r) => ({
      value: r.name,
      label: r.name,
    }));
    const known = new Set(available.map((o) => o.value));
    const merged = [...available];
    for (const r of rolesValue) {
      if (!known.has(r)) merged.push({ value: r, label: r });
    }
    return merged;
  }, [rolesList.data, rolesValue]);

  return (
    <Drawer
      open
      onOpenChange={(o) => {
        if (!o) onClose();
      }}
    >
      <DrawerContent>
        <DrawerTitle>{isCreate ? "New user" : "Edit user"}</DrawerTitle>
        <DrawerBody>
          {!isCreate && existing.isPending ? (
            <div style={{ display: "flex", justifyContent: "center", padding: "var(--space-5)" }}>
              <Spinner size={20} />
            </div>
          ) : (
            <form
              id="user-form"
              className={styles.stack}
              onSubmit={(e) => void handleSubmit(onSubmit)(e)}
            >
              <section className={styles.section}>
                <h3 className={styles.sectionTitle}>Identity</h3>
                <div className={styles.field}>
                  <label className={styles.label} htmlFor="user-name">
                    Name
                  </label>
                  <Input
                    id="user-name"
                    {...register("name")}
                    invalid={nameInvalid}
                    placeholder="e.g. alice"
                  />
                </div>
                <div className={styles.field}>
                  <label className={styles.label} htmlFor="user-type">
                    Type
                  </label>
                  <Select
                    value={typeValue}
                    onValueChange={(v) =>
                      setValue("type", v as "local" | "ldap", { shouldDirty: true })
                    }
                  >
                    <SelectTrigger />
                    <SelectContent>
                      <SelectItem value="local">local</SelectItem>
                      <SelectItem value="ldap">ldap</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
                <div className={styles.field}>
                  <span className={styles.label}>Roles</span>
                  <MultiCombobox
                    aria-label="Roles"
                    placeholder="Select one or more roles"
                    options={roleOptions}
                    value={rolesValue}
                    onChange={(next) => setValue("roles", next, { shouldDirty: true })}
                    allowCustom
                  />
                </div>
                {isCreate && (
                  <div className={styles.field}>
                    <label className={styles.label} htmlFor="user-password">
                      Password
                    </label>
                    <Input id="user-password" type="password" {...register("password")} />
                  </div>
                )}
              </section>
              <div className={styles.field}>
                <label className={styles.label} htmlFor="user-comment">
                  Comment
                </label>
                <Textarea
                  id="user-comment"
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
            form="user-form"
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
