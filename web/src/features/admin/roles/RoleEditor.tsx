import { useMemo } from "react";
import { useWatch } from "react-hook-form";
import { EditorDrawer, useFieldInvalid, type EditorBodyProps } from "@/shared/forms/EditorDrawer";
import { Input } from "@/shared/ui/Input";
import { MultiCombobox } from "@/shared/ui/MultiCombobox";
import { Textarea } from "@/shared/ui/Textarea";
import { Roles, usePermissionsCatalogue } from "./api";
import type { Role } from "./types";
import styles from "./RoleEditor.module.css";

type FormShape = {
  name: string;
  permissions: string[];
  groups: string[];
  comment: string;
};

const EMPTY_FORM: FormShape = {
  name: "",
  permissions: [],
  groups: [],
  comment: "",
};

export type RoleEditorProps = {
  uid: string | undefined;
  onClose: () => void;
};

export function RoleEditor({ uid, onClose }: RoleEditorProps) {
  const isCreate = uid === undefined || uid === "";
  const get = Roles.useGet(isCreate ? undefined : uid);
  const create = Roles.useCreate();
  const update = Roles.useUpdate();

  return (
    <EditorDrawer<FormShape, Role>
      uid={uid}
      onClose={onClose}
      get={get}
      create={create}
      update={update}
      emptyForm={EMPTY_FORM}
      recordToForm={(role) => ({
        name: role.name ?? "",
        permissions: role.permissions ?? [],
        groups: role.groups ?? [],
        comment: role.comment ?? "",
      })}
      formToBody={(form) => ({
        name: form.name,
        permissions: form.permissions,
        groups: form.groups,
        ...(form.comment ? { comment: form.comment } : {}),
      })}
      title={(c) => (c ? "New role" : "Edit role")}
      successMessage={{ create: "Role created", update: "Role saved" }}
      formId="role-form"
      formClassName={styles.stack}
    >
      {(body) => <RoleFields {...body} />}
    </EditorDrawer>
  );
}

function RoleFields({ register, control, setValue }: EditorBodyProps<FormShape>) {
  const catalogue = usePermissionsCatalogue();
  const nameInvalid = useFieldInvalid(control, "name");
  const permissions = useWatch({ control, name: "permissions" });
  const groups = useWatch({ control, name: "groups" });

  // Groups are free-form values from the auth backend (LDAP CNs, OIDC app-role
  // / group-claim strings). There is no server catalogue, so the options are
  // just whatever the role already has; allowCustom lets the admin type new
  // ones (e.g. "GrafanaAdmin").
  const groupOptions = useMemo(() => groups.map((g) => ({ value: g, label: g })), [groups]);

  // Merge the catalogue with any permissions already on the role so a
  // legacy/unknown value still renders as a badge and survives a Save
  // round-trip. Mirrors the pattern used by UserEditor for roles.
  const catalogueData = catalogue.data;
  const permissionOptions = useMemo(() => {
    const known = catalogueData ?? [];
    const seen = new Set(known);
    const merged = known.map((p) => ({ value: p, label: p }));
    for (const p of permissions) {
      if (!seen.has(p)) {
        merged.push({ value: p, label: p });
        seen.add(p);
      }
    }
    return merged;
  }, [catalogueData, permissions]);

  return (
    <>
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
        <div className={styles.field}>
          <span className={styles.label} id="role-groups-label">
            Groups
          </span>
          <MultiCombobox
            aria-label="Groups"
            placeholder="Map auth-backend groups (e.g. GrafanaAdmin) to this role"
            options={groupOptions}
            value={groups}
            onChange={(next) => setValue("groups", next, { shouldDirty: true })}
            allowCustom
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
    </>
  );
}
