import { EditorDrawer, useFieldInvalid, type EditorBodyProps } from "@/shared/forms/EditorDrawer";
import { Input } from "@/shared/ui/Input";
import { Textarea } from "@/shared/ui/Textarea";
import { KVs } from "./api";
import type { KV } from "./types";
import styles from "./KVEditor.module.css";

type FormShape = {
  dict: string;
  key: string;
  value: string;
  comment: string;
};

const EMPTY_FORM: FormShape = {
  dict: "",
  key: "",
  value: "",
  comment: "",
};

export type KVEditorProps = {
  uid: string | undefined;
  onClose: () => void;
};

export function KVEditor({ uid, onClose }: KVEditorProps) {
  const isCreate = uid === undefined || uid === "";
  const get = KVs.useGet(isCreate ? undefined : uid);
  const create = KVs.useCreate();
  const update = KVs.useUpdate();

  return (
    <EditorDrawer<FormShape, KV>
      uid={uid}
      onClose={onClose}
      get={get}
      create={create}
      update={update}
      emptyForm={EMPTY_FORM}
      recordToForm={(kv) => ({
        dict: kv.dict ?? "",
        key: kv.key ?? "",
        value: kv.value ?? "",
        comment: kv.comment ?? "",
      })}
      formToBody={(form) => ({
        dict: form.dict,
        key: form.key,
        ...(form.value ? { value: form.value } : {}),
        ...(form.comment ? { comment: form.comment } : {}),
      })}
      title={(c) => (c ? "New key-value" : "Edit key-value")}
      successMessage={{ create: "Key-value created", update: "Key-value saved" }}
      formId="kv-form"
      formClassName={styles.stack}
    >
      {(body) => <KVFields {...body} />}
    </EditorDrawer>
  );
}

function KVFields({ register, control }: EditorBodyProps<FormShape>) {
  const dictInvalid = useFieldInvalid(control, "dict");
  const keyInvalid = useFieldInvalid(control, "key");
  return (
    <section className={styles.section}>
      <h3 className={styles.sectionTitle}>Identity</h3>
      <div className={styles.field}>
        <label className={styles.label} htmlFor="kv-dict">
          Dictionary
        </label>
        <Input
          id="kv-dict"
          {...register("dict")}
          invalid={dictInvalid}
          placeholder="e.g. host_owner_lookup"
        />
      </div>
      <div className={styles.field}>
        <label className={styles.label} htmlFor="kv-key">
          Key
        </label>
        <Input
          id="kv-key"
          {...register("key")}
          invalid={keyInvalid}
          placeholder="e.g. MY_SETTING"
        />
      </div>
      <div className={styles.field}>
        <label className={styles.label} htmlFor="kv-value">
          Value
        </label>
        <Textarea id="kv-value" {...register("value")} rows={4} placeholder="Optional value" />
      </div>
      <div className={styles.field}>
        <label className={styles.label} htmlFor="kv-comment">
          Comment
        </label>
        <Textarea
          id="kv-comment"
          {...register("comment")}
          rows={2}
          placeholder="Optional description"
        />
      </div>
    </section>
  );
}
