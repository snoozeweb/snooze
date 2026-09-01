import { useMemo } from "react";
import { useWatch } from "react-hook-form";
import { EditorDrawer, useFieldInvalid, type EditorBodyProps } from "@/shared/forms/EditorDrawer";
import { PermissionsCombobox } from "@/shared/forms/PermissionsCombobox";
import { Input } from "@/shared/ui/Input";
import { MultiCombobox, type MultiComboboxOption } from "@/shared/ui/MultiCombobox";
import { Textarea } from "@/shared/ui/Textarea";
import { Roles, usePermissionsCatalogue } from "./api";
import { Groups } from "@/features/admin/groups/api";
import { Users } from "@/features/admin/users/api";
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

  // The group→role mapping matches an auth-backend group string. There's no
  // single server catalogue for those, so we suggest from the two places groups
  // actually come from: groups seen on real user identities (LDAP CNs / OIDC
  // claims populated at login) and groups an admin defined in the Groups menu.
  // allowCustom still lets them type one that hasn't surfaced yet.
  const definedGroups = Groups.useList({ limit: 500, orderby: "name", asc: true });
  const usersList = Users.useList({ limit: 500 });

  const groupOptions = useMemo<MultiComboboxOption[]>(() => {
    // name → source description. Fill discovered first (lower precedence), then
    // let a defined group override with its own description.
    const byName = new Map<string, string | undefined>();
    for (const u of usersList.data?.data ?? []) {
      for (const g of u.groups ?? []) {
        if (!byName.has(g)) byName.set(g, "Discovered on users");
      }
    }
    for (const g of definedGroups.data?.data ?? []) {
      byName.set(g.name, g.description ? `Group · ${g.description}` : "Group");
    }
    // Keep already-mapped groups present even if neither source lists them, so
    // they render as badges and survive a save.
    for (const g of groups) {
      if (!byName.has(g)) byName.set(g, undefined);
    }
    return [...byName.entries()]
      .sort((a, b) => a[0].localeCompare(b[0]))
      .map(([value, description]) =>
        description ? { value, label: value, description } : { value, label: value },
      );
  }, [definedGroups.data, usersList.data, groups]);

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
            errorMessage={nameInvalid ? "Name is required." : undefined}
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
          <PermissionsCombobox
            available={catalogue.data ?? []}
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
            placeholder="Pick a group or type an auth-backend group (e.g. GrafanaAdmin)"
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
