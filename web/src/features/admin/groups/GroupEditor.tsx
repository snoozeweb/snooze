import { useState } from "react";
import { useWatch } from "react-hook-form";
import { EditorDrawer, useFieldInvalid, type EditorBodyProps } from "@/shared/forms/EditorDrawer";
import { Input } from "@/shared/ui/Input";
import { Badge } from "@/shared/ui/Badge";
import { Button } from "@/shared/ui/Button";
import { Code } from "@/shared/ui/Code";
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
  const [username, setUsername] = useState("");
  const [method, setMethod] = useState("local");
  const [customMethod, setCustomMethod] = useState("");

  const effectiveMethod = method === "__other__" ? customMethod : method;

  const addMember = () => {
    if (!username.trim() || !effectiveMethod.trim()) return;
    const next = members.filter(
      (m) => !(m.username === username.trim() && m.method === effectiveMethod.trim()),
    );
    next.push({ username: username.trim(), method: effectiveMethod.trim() });
    setValue("members", next, { shouldDirty: true });
    setUsername("");
    setMethod("local");
    setCustomMethod("");
  };

  return (
    <div className={styles.memberSection}>
      <span className={styles.label}>Members</span>
      <ul className={styles.memberList}>
        {members.map((m) => (
          <li key={`${m.username}:${m.method}`} className={styles.memberRow}>
            <Code>{m.username}</Code>
            <Badge variant="neutral">{m.method}</Badge>
            <button
              type="button"
              aria-label={`Remove ${m.username} (${m.method})`}
              onClick={() =>
                setValue(
                  "members",
                  members.filter((x) => !(x.username === m.username && x.method === m.method)),
                  { shouldDirty: true },
                )
              }
            >
              &times;
            </button>
          </li>
        ))}
      </ul>
      <div className={styles.addRow}>
        <Input
          aria-label="Username"
          value={username}
          onChange={(e) => setUsername(e.target.value)}
          placeholder="username"
        />
        <select aria-label="Method" value={method} onChange={(e) => setMethod(e.target.value)}>
          <option value="local">local</option>
          <option value="ldap">ldap</option>
          <option value="__other__">Other&hellip;</option>
        </select>
        {method === "__other__" && (
          <Input
            aria-label="Custom method"
            value={customMethod}
            onChange={(e) => setCustomMethod(e.target.value)}
            placeholder="e.g. microsoft"
          />
        )}
        <Button type="button" size="sm" variant="secondary" onClick={addMember}>
          Add member
        </Button>
      </div>
    </div>
  );
}
