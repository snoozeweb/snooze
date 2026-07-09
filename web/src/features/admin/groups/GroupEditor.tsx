import { useWatch } from "react-hook-form";
import { EditorDrawer, useFieldInvalid, type EditorBodyProps } from "@/shared/forms/EditorDrawer";
import { UsersMultiSelect } from "@/shared/forms/UsersMultiSelect";
import { Input } from "@/shared/ui/Input";
import { Groups } from "./api";
import type { Group, GroupMember } from "./types";
import styles from "./GroupEditor.module.css";

type FormShape = {
  name: string;
  description: string;
  members: GroupMember[];
};

const EMPTY_FORM: FormShape = { name: "", description: "", members: [] };

export type GroupEditorProps = {
  uid: string | undefined;
  onClose: () => void;
};

export function GroupEditor({ uid, onClose }: GroupEditorProps) {
  const isCreate = uid === undefined || uid === "";
  const get = Groups.useGet(isCreate ? undefined : uid);
  const create = Groups.useCreate();
  const update = Groups.useUpdate();

  return (
    <EditorDrawer<FormShape, Group>
      uid={uid}
      onClose={onClose}
      get={get}
      create={create}
      update={update}
      emptyForm={EMPTY_FORM}
      recordToForm={(g) => ({
        name: g.name ?? "",
        description: g.description ?? "",
        members: g.members ?? [],
      })}
      formToBody={(form) => ({
        name: form.name,
        ...(form.description ? { description: form.description } : {}),
        members: form.members,
      })}
      title={(c) => (c ? "New group" : "Edit group")}
      successMessage={{ create: "Group created", update: "Group saved" }}
      formId="group-form"
      formClassName={styles.stack}
    >
      {(body) => <GroupFields {...body} />}
    </EditorDrawer>
  );
}

function GroupFields({ register, control, setValue }: EditorBodyProps<FormShape>) {
  const nameInvalid = useFieldInvalid(control, "name");

  return (
    <>
      <div className={styles.field}>
        <label className={styles.label} htmlFor="group-name">
          Name
        </label>
        <Input
          id="group-name"
          {...register("name")}
          invalid={nameInvalid}
          placeholder="e.g. sre"
          aria-label="Name"
        />
      </div>
      <div className={styles.field}>
        <label className={styles.label} htmlFor="group-description">
          Description
        </label>
        <Input
          id="group-description"
          {...register("description")}
          placeholder="Optional description"
          aria-label="Description"
        />
      </div>
      <MemberList control={control} setValue={setValue} />
    </>
  );
}

function MemberList({
  control,
  setValue,
}: Pick<EditorBodyProps<FormShape>, "control" | "setValue">) {
  const members = useWatch({ control, name: "members" });

  return (
    <div className={styles.field}>
      {/* UsersMultiSelect is not a native control, so the visible label is
          cosmetic — the picker is reached via its aria-label ("Members"). */}
      <span className={styles.label} id="group-members-label">
        Members
      </span>
      <UsersMultiSelect
        aria-label="Members"
        placeholder="Select users to add to this group"
        value={members}
        onChange={(next) => setValue("members", next, { shouldDirty: true })}
      />
    </div>
  );
}
