import { useWatch } from "react-hook-form";
import { EditorDrawer, useFieldInvalid, type EditorBodyProps } from "@/shared/forms/EditorDrawer";
import { ConditionPreview } from "@/shared/ui/ConditionPreview";
import { Input } from "@/shared/ui/Input";
import { Textarea } from "@/shared/ui/Textarea";
import { ConditionEditor } from "@/shared/condition/ConditionEditor";
import type { Condition } from "@/lib/condition/types";
import { Environments } from "./api";
import type { Environment } from "./types";
import styles from "./EnvironmentEditor.module.css";

type FormShape = {
  name: string;
  color: string;
  comment: string;
  condition: Condition;
  tree_order: number;
};

const EMPTY_FORM: FormShape = {
  name: "",
  color: "#000000",
  comment: "",
  condition: { type: "ALWAYS_TRUE" },
  tree_order: 0,
};

export type EnvironmentEditorProps = {
  uid: string | undefined;
  onClose: () => void;
};

function isAlwaysTrue(c: Condition | undefined): boolean {
  return !c || c.type === "ALWAYS_TRUE";
}

export function EnvironmentEditor({ uid, onClose }: EnvironmentEditorProps) {
  const isCreate = uid === undefined || uid === "";
  const get = Environments.useGet(isCreate ? undefined : uid);
  const create = Environments.useCreate();
  const update = Environments.useUpdate();

  return (
    <EditorDrawer<FormShape, Environment>
      uid={uid}
      onClose={onClose}
      get={get}
      create={create}
      update={update}
      emptyForm={EMPTY_FORM}
      recordToForm={(env) => ({
        name: env.name ?? "",
        color: env.color ?? "#000000",
        comment: env.comment ?? "",
        condition: env.condition ?? { type: "ALWAYS_TRUE" },
        tree_order: env.tree_order ?? 0,
      })}
      formToBody={(form) => ({
        name: form.name,
        ...(form.color ? { color: form.color } : {}),
        ...(form.comment ? { comment: form.comment } : {}),
        ...(isAlwaysTrue(form.condition) ? {} : { condition: form.condition }),
        ...(Number.isFinite(form.tree_order) ? { tree_order: form.tree_order } : {}),
      })}
      title={(c) => (c ? "New environment" : "Edit environment")}
      successMessage={{ create: "Environment created", update: "Environment saved" }}
      formId="environment-form"
      formClassName={styles.stack}
    >
      {(body) => <EnvironmentFields {...body} />}
    </EditorDrawer>
  );
}

function EnvironmentFields({ register, control, setValue }: EditorBodyProps<FormShape>) {
  const nameInvalid = useFieldInvalid(control, "name");
  const condition = useWatch({ control, name: "condition" });
  return (
    <>
      <section className={styles.section}>
        <h3 className={styles.sectionTitle}>Identity</h3>
        <div className={styles.field}>
          <label className={styles.label} htmlFor="environment-name">
            Name
          </label>
          <Input
            id="environment-name"
            {...register("name")}
            invalid={nameInvalid}
            placeholder="e.g. production"
          />
        </div>
        <div className={styles.field}>
          <label className={styles.label} htmlFor="environment-color">
            Color
          </label>
          <input id="environment-color" type="color" {...register("color")} />
        </div>
        <div className={styles.field}>
          <label className={styles.label} htmlFor="environment-tree-order">
            Tree order
          </label>
          <Input
            id="environment-tree-order"
            type="number"
            {...register("tree_order", { valueAsNumber: true })}
          />
        </div>
      </section>
      <section className={styles.section}>
        <h3 className={styles.sectionTitle}>Filter</h3>
        <ConditionEditor
          value={condition}
          onChange={(c) => setValue("condition", c, { shouldDirty: true })}
          plugin="record"
        />
        <div style={{ marginTop: "var(--space-2)" }}>
          <ConditionPreview condition={condition} />
        </div>
      </section>
      <div className={styles.field}>
        <label className={styles.label} htmlFor="environment-comment">
          Comment
        </label>
        <Textarea
          id="environment-comment"
          {...register("comment")}
          rows={2}
          placeholder="Optional description"
        />
      </div>
    </>
  );
}
