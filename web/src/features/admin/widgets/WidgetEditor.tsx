import { useEffect, useMemo, useState } from "react";
import { useWatch, type UseFormRegister } from "react-hook-form";
import {
  EditorAbort,
  EditorDrawer,
  useFieldInvalid,
  type EditorBodyProps,
} from "@/shared/forms/EditorDrawer";
import { Input } from "@/shared/ui/Input";
import { Textarea } from "@/shared/ui/Textarea";
import { Widgets } from "./api";
import { KNOWN_WIDGETS, findWidgetDef, type WidgetDef, type WidgetField } from "./catalogue";
import type { Widget } from "./types";
import styles from "./WidgetEditor.module.css";

// FormShape holds every editable surface. Which ones are actually used at
// submit time depends on `subtypeSelection`:
//   - a known catalogue type → typed `config` map drives the submitted body
//   - "" (Other)             → `customWidgetType` + `config_json` drive it
//
// We keep both pieces of state alive (rather than tearing them down on each
// dropdown change) so an operator can flip between modes without losing the
// values they typed; whichever mode is active at submit-time wins.
type FormShape = {
  name: string;
  subtypeSelection: string; // catalogue type or "" for Other
  customWidgetType: string;
  config: Record<string, string>; // raw input strings; coerced at submit
  config_json: string;
  comment: string;
};

const EMPTY_FORM: FormShape = {
  name: "",
  subtypeSelection: "",
  customWidgetType: "",
  config: {},
  config_json: "{}",
  comment: "",
};

const selectStyle: React.CSSProperties = {
  height: 28,
  fontSize: "var(--text-sm)",
  background: "var(--bg-surface)",
  color: "var(--text-strong)",
  border: "1px solid var(--border)",
  borderRadius: "var(--radius-md)",
  padding: "0 var(--space-2)",
  width: "100%",
};

export type WidgetEditorProps = {
  uid: string | undefined;
  onClose: () => void;
};

export function WidgetEditor({ uid, onClose }: WidgetEditorProps) {
  const isCreate = uid === undefined || uid === "";
  const get = Widgets.useGet(isCreate ? undefined : uid);
  const create = Widgets.useCreate();
  const update = Widgets.useUpdate();

  // jsonError lives here (not in the body render prop) because formToBody —
  // which runs in the frame's submit handler — needs to set it before
  // throwing EditorAbort, and the body needs to read it to render the inline
  // message. It is cleared at the start of every submit.
  const [jsonError, setJsonError] = useState<string | null>(null);

  return (
    <EditorDrawer<FormShape, Widget>
      uid={uid}
      onClose={onClose}
      get={get}
      create={create}
      update={update}
      emptyForm={EMPTY_FORM}
      recordToForm={(w) => {
        const existingType = w.widget_type ?? "";
        const def = findWidgetDef(existingType);
        const cfg: Record<string, unknown> = w.config ?? {};
        // Project known-typed fields into string form for the inputs. Unknown
        // keys are preserved through the JSON textarea fallback.
        const typedConfig: Record<string, string> = {};
        if (def) {
          for (const field of def.fields) {
            const v = cfg[field.name];
            if (v === undefined || v === null) continue;
            // Coerce primitives explicitly: String(object) would yield
            // "[object Object]" which is never what a widget config field
            // should be displayed as. We just skip non-primitive values.
            if (typeof v === "string") typedConfig[field.name] = v;
            else if (typeof v === "number" || typeof v === "boolean")
              typedConfig[field.name] = String(v);
          }
        }
        return {
          name: w.name ?? "",
          subtypeSelection: def ? def.type : "",
          customWidgetType: def ? "" : existingType,
          config: typedConfig,
          config_json: JSON.stringify(cfg, null, 2),
          comment: w.comment ?? "",
        };
      }}
      formToBody={(form) => {
        setJsonError(null);
        const selectedDef = findWidgetDef(form.subtypeSelection);
        let widgetType: string | undefined;
        let configObj: Record<string, unknown>;

        if (selectedDef) {
          widgetType = selectedDef.type;
          configObj = {};
          for (const field of selectedDef.fields) {
            const raw = form.config[field.name];
            const provided = raw !== undefined && raw !== "";
            if (!provided) {
              if (field.default !== undefined) configObj[field.name] = field.default;
              continue;
            }
            if (field.kind === "int") {
              const n = Number(raw);
              if (!Number.isFinite(n)) {
                setJsonError(`${field.label}: expected a number`);
                throw new EditorAbort();
              }
              configObj[field.name] = Math.trunc(n);
            } else {
              configObj[field.name] = raw;
            }
          }
        } else {
          widgetType = form.customWidgetType.trim() || undefined;
          try {
            const parsed = JSON.parse(form.config_json) as unknown;
            if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) {
              throw new Error("expected a JSON object");
            }
            configObj = parsed as Record<string, unknown>;
          } catch (e) {
            setJsonError(e instanceof Error ? e.message : "Invalid JSON");
            throw new EditorAbort();
          }
        }

        const body: Widget = {
          name: form.name,
          ...(widgetType ? { widget_type: widgetType } : {}),
          config: configObj,
          ...(form.comment ? { comment: form.comment } : {}),
        };
        return body;
      }}
      title={(c) => (c ? "New widget" : "Edit widget")}
      successMessage={{ create: "Widget created", update: "Widget saved" }}
      formId="widget-form"
      formClassName={styles.stack}
    >
      {(body) => <WidgetFields {...body} jsonError={jsonError} />}
    </EditorDrawer>
  );
}

type WidgetFieldsProps = EditorBodyProps<FormShape> & {
  jsonError: string | null;
};

function WidgetFields({ control, register, setValue, jsonError }: WidgetFieldsProps) {
  const nameInvalid = useFieldInvalid(control, "name");
  const subtypeSelection = useWatch({ control, name: "subtypeSelection" });
  const configValues = useWatch({ control, name: "config" });
  const selectedDef = useMemo<WidgetDef | undefined>(
    () => findWidgetDef(subtypeSelection),
    [subtypeSelection],
  );

  // When the catalogue selection changes to a known def, seed defaults into
  // the typed config map for any field that hasn't been set yet.
  useEffect(() => {
    if (!selectedDef) return;
    for (const field of selectedDef.fields) {
      if (configValues[field.name] !== undefined && configValues[field.name] !== "") continue;
      if (field.default !== undefined) {
        setValue(`config.${field.name}`, String(field.default), { shouldDirty: false });
      }
    }
    // configValues intentionally omitted: we only want this to run when the
    // selected def changes, not on every keystroke (which would clobber edits).
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedDef, setValue]);

  return (
    <>
      <section className={styles.section}>
        <h3 className={styles.sectionTitle}>Identity</h3>
        <div className={styles.field}>
          <label className={styles.label} htmlFor="widget-name">
            Name
          </label>
          <Input
            id="widget-name"
            {...register("name")}
            invalid={nameInvalid}
            placeholder="e.g. patlite-floor1"
          />
        </div>
        <div className={styles.field}>
          <label className={styles.label} htmlFor="widget-type">
            Widget type
          </label>
          <select
            id="widget-type"
            {...register("subtypeSelection")}
            style={selectStyle}
            className={styles.widgetTypeSelect}
          >
            {KNOWN_WIDGETS.map((w) => (
              <option key={w.type} value={w.type}>
                {w.displayName}
              </option>
            ))}
            <option value="">Other (free-form)</option>
          </select>
          {selectedDef?.description ? (
            <span style={{ color: "var(--text-muted)", fontSize: "var(--text-xs)" }}>
              {selectedDef.description}
            </span>
          ) : null}
        </div>
        {!selectedDef ? (
          <div className={styles.field}>
            <label className={styles.label} htmlFor="widget-type-custom">
              Custom type
            </label>
            <Input
              id="widget-type-custom"
              {...register("customWidgetType")}
              placeholder="iframe | grafana | …"
            />
          </div>
        ) : null}
      </section>

      {selectedDef ? (
        <section className={styles.section}>
          <h3 className={styles.sectionTitle}>Config</h3>
          {selectedDef.fields.map((field) => (
            <TypedFieldInput key={field.name} field={field} register={register} />
          ))}
        </section>
      ) : (
        <section className={styles.section}>
          <h3 className={styles.sectionTitle}>Config (JSON)</h3>
          <Textarea
            {...register("config_json")}
            rows={10}
            invalid={!!jsonError}
            style={{ fontFamily: "var(--font-mono)" }}
            aria-label="Config (JSON)"
          />
          {jsonError ? (
            <span style={{ color: "var(--severity-critical)", fontSize: "var(--text-xs)" }}>
              {jsonError}
            </span>
          ) : null}
        </section>
      )}

      {selectedDef && jsonError ? (
        <span style={{ color: "var(--severity-critical)", fontSize: "var(--text-xs)" }}>
          {jsonError}
        </span>
      ) : null}

      <div className={styles.field}>
        <label className={styles.label} htmlFor="widget-comment">
          Comment
        </label>
        <Textarea id="widget-comment" {...register("comment")} rows={2} />
      </div>
    </>
  );
}

type TypedFieldInputProps = {
  field: WidgetField;
  register: UseFormRegister<FormShape>;
};

function TypedFieldInput({ field, register }: TypedFieldInputProps) {
  const inputId = `widget-cfg-${field.name}`;
  return (
    <div className={styles.field}>
      <label className={styles.label} htmlFor={inputId}>
        {field.label}
        {field.required ? <span aria-hidden="true"> *</span> : null}
      </label>
      <Input
        id={inputId}
        type={field.kind === "int" ? "number" : "text"}
        {...register(`config.${field.name}`)}
        placeholder={field.default !== undefined ? String(field.default) : undefined}
      />
      {field.description ? (
        <span style={{ color: "var(--text-muted)", fontSize: "var(--text-xs)" }}>
          {field.description}
        </span>
      ) : null}
    </div>
  );
}
