import { useMemo } from "react";
import { useWatch } from "react-hook-form";
import { EditorDrawer, useFieldInvalid, type EditorBodyProps } from "@/shared/forms/EditorDrawer";
import { Input } from "@/shared/ui/Input";
import { MultiCombobox } from "@/shared/ui/MultiCombobox";
import { Switch } from "@/shared/ui/Switch";
import { Textarea } from "@/shared/ui/Textarea";
import { Select, SelectContent, SelectItem, SelectTrigger } from "@/shared/ui/Select";
import { Roles } from "@/features/admin/roles/api";
import { Users } from "./api";
import type { User } from "./types";
import styles from "./UserEditor.module.css";

type FormShape = {
  name: string;
  // Canonical auth method: "local"/"ldap" for create, plus any OIDC method
  // (e.g. "microsoft") shown read-only when editing an SSO user.
  method: string;
  roles: string[];
  comment: string;
  password: string;
  enabled: boolean;
};

const EMPTY_FORM: FormShape = {
  name: "",
  method: "local",
  roles: [],
  comment: "",
  password: "",
  enabled: true,
};

export type UserEditorProps = {
  uid: string | undefined;
  onClose: () => void;
};

export function UserEditor({ uid, onClose }: UserEditorProps) {
  const isCreate = uid === undefined || uid === "";
  const get = Users.useGet(isCreate ? undefined : uid);
  const create = Users.useCreate();
  const update = Users.useUpdate();

  return (
    <EditorDrawer<FormShape, User>
      uid={uid}
      onClose={onClose}
      get={get}
      create={create}
      update={update}
      emptyForm={EMPTY_FORM}
      recordToForm={(u) => ({
        name: u.name ?? "",
        method: u.method ?? u.type ?? "local",
        roles: u.roles ?? [],
        comment: u.comment ?? "",
        password: "",
        // Absent enabled is treated as enabled (legacy/seeded docs).
        enabled: u.enabled ?? true,
      })}
      formToBody={(form, create) => {
        // Empty password = "do not change". The backend's WriteTransformer
        // already drops an empty value from the PATCH, but pruning it
        // client-side keeps the audit summary tidy (the patch field list
        // omits `password` instead of recording a no-op write).
        const body: User = {
          name: form.name,
          ...(form.roles.length ? { roles: form.roles } : {}),
          ...(form.comment ? { comment: form.comment } : {}),
          ...(form.password ? { password: form.password } : {}),
          enabled: form.enabled,
        };
        // method is the canonical backend field; it is the user's immutable
        // identity component, so it is set on create and never sent on edit.
        if (create) body.method = form.method;
        return body;
      }}
      title={(c) => (c ? "New user" : "Edit user")}
      successMessage={{ create: "User created", update: "User saved" }}
      formId="user-form"
      formClassName={styles.stack}
    >
      {(body) => <UserFields {...body} />}
    </EditorDrawer>
  );
}

function UserFields({ register, control, setValue, isCreate }: EditorBodyProps<FormShape>) {
  const nameInvalid = useFieldInvalid(control, "name");
  const methodValue = useWatch({ control, name: "method" });
  const enabledValue = useWatch({ control, name: "enabled" });
  const rolesValue = useWatch({ control, name: "roles" });

  const rolesList = Roles.useList({ limit: 500 });
  const rolesData = rolesList.data;
  const roleOptions = useMemo(() => {
    const available = (rolesData?.data ?? []).map((r) => ({
      value: r.name,
      label: r.name,
    }));
    const known = new Set(available.map((o) => o.value));
    const merged = [...available];
    for (const r of rolesValue) {
      if (!known.has(r)) merged.push({ value: r, label: r });
    }
    return merged;
  }, [rolesData, rolesValue]);

  return (
    <>
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
            autoComplete="username"
            spellCheck={false}
          />
        </div>
        <div className={styles.field}>
          <label className={styles.label} htmlFor="user-type">
            Type
          </label>
          {isCreate ? (
            <Select
              value={methodValue}
              onValueChange={(v) => setValue("method", v, { shouldDirty: true })}
            >
              <SelectTrigger id="user-type" />
              <SelectContent>
                <SelectItem value="local">local</SelectItem>
                <SelectItem value="ldap">ldap</SelectItem>
              </SelectContent>
            </Select>
          ) : (
            // A user's auth method is immutable, so it is read-only when
            // editing — including SSO users (e.g. "microsoft").
            <Input id="user-type" value={methodValue} readOnly disabled />
          )}
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
        <div className={styles.field}>
          <span className={styles.label}>Status</span>
          <div
            style={{
              display: "flex",
              flexDirection: "row",
              alignItems: "center",
              gap: "var(--space-2)",
            }}
          >
            <Switch
              id="user-enabled"
              checked={enabledValue}
              onCheckedChange={(v) => setValue("enabled", v, { shouldDirty: true })}
              aria-label="Enabled"
            />
            <label className={styles.label} htmlFor="user-enabled" style={{ margin: 0 }}>
              {enabledValue ? "Enabled" : "Disabled — login blocked"}
            </label>
          </div>
        </div>
      </section>
      {methodValue === "local" ? (
        <div className={styles.field}>
          <label className={styles.label} htmlFor="user-password">
            Password
          </label>
          <Input
            id="user-password"
            type="password"
            autoComplete="new-password"
            placeholder={isCreate ? "" : "Leave blank to keep current password"}
            {...register("password")}
          />
        </div>
      ) : null}
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
    </>
  );
}
