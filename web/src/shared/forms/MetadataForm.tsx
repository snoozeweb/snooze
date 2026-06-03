import { useEffect, useMemo, useState, type ReactElement } from "react";
import { Input } from "@/shared/ui/Input";
import { Switch } from "@/shared/ui/Switch";
import { Textarea } from "@/shared/ui/Textarea";
import type { FormField, FormFieldOption } from "./types";
import styles from "./MetadataForm.module.css";

export type MetadataFormProps = {
  fields: Record<string, FormField>;
  value: Record<string, unknown>;
  onChange: (next: Record<string, unknown>) => void;
  idPrefix?: string;
  disabled?: boolean;
};

export function MetadataForm({
  fields,
  value,
  onChange,
  idPrefix = "mf",
  disabled = false,
}: MetadataFormProps) {
  // Seed default_value into the parent state once per field key, only when
  // the current value is undefined. We do it via an effect so the parent
  // owns canonical state from the first render onward.
  useEffect(() => {
    const next: Record<string, unknown> = { ...value };
    let mutated = false;
    for (const [key, field] of Object.entries(fields)) {
      if (next[key] === undefined && field.default_value !== undefined) {
        next[key] = field.default_value;
        mutated = true;
      }
    }
    if (mutated) onChange(next);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [fields]);

  function setField(key: string, v: unknown) {
    onChange({ ...value, [key]: v });
  }

  return (
    <div className={styles.stack}>
      {Object.entries(fields).map(([key, field]) => {
        const fid = `${idPrefix}-${key}`;
        const current = value[key] ?? field.default_value;
        return (
          <div key={key} className={styles.field}>
            <label className={styles.label} htmlFor={fid}>
              {field.display_name}
              {field.required ? <span className={styles.required}>*</span> : null}
            </label>
            <MetadataField
              id={fid}
              field={field}
              value={current}
              onChange={(v) => setField(key, v)}
              disabled={disabled}
            />
            {field.description ? <span className={styles.help}>{field.description}</span> : null}
          </div>
        );
      })}
    </div>
  );
}

export type MetadataFieldProps = {
  field: FormField;
  value: unknown;
  onChange: (v: unknown) => void;
  id?: string;
  disabled?: boolean;
};

/**
 * MetadataField renders exactly one form control for a FormField descriptor.
 * It's the per-field renderer extracted from MetadataForm — useful when a
 * caller already owns the label / description layout (e.g. the Settings
 * editor renders one selected field at a time) and only needs the input.
 *
 * The component is intentionally label-less: the caller supplies the
 * <label>, the description text, and any required-asterisk decoration.
 */
export function MetadataField({
  field,
  value,
  onChange,
  id,
  disabled = false,
}: MetadataFieldProps): ReactElement {
  // Synthesise a stable-ish id when the caller doesn't supply one. We need
  // an id for the Arguments key/value rows (placeholders carry a "name-${i}"
  // suffix) and to satisfy `htmlFor` on parent labels.
  //
  // We deliberately do NOT fall back to `field.default_value` when `value` is
  // undefined: the caller owns canonical state. MetadataForm seeds defaults
  // into the parent map up front; standalone callers (e.g. SettingEditor)
  // seed once when they pick a key. Falling back here would make `clear()`
  // followed by re-typing snap back to the default mid-edit.
  const fid = id ?? `mf-${field.display_name.replace(/\s+/g, "-").toLowerCase()}`;
  return (
    <FieldControl id={fid} field={field} value={value} onChange={onChange} disabled={disabled} />
  );
}

type ControlProps = {
  id: string;
  field: FormField;
  value: unknown;
  onChange: (v: unknown) => void;
  disabled: boolean;
};

function FieldControl({ id, field, value, onChange, disabled }: ControlProps) {
  switch (field.component) {
    case "String":
      return (
        <Input
          id={id}
          type="text"
          value={asString(value)}
          onChange={(e) => onChange(e.target.value)}
          disabled={disabled}
        />
      );
    case "Password":
      return (
        <Input
          id={id}
          type="password"
          value={asString(value)}
          onChange={(e) => onChange(e.target.value)}
          disabled={disabled}
        />
      );
    case "Number":
      return (
        <Input
          id={id}
          type="number"
          value={asString(value)}
          onChange={(e) => {
            const t = e.target.value;
            if (t === "") onChange(undefined);
            else {
              const n = Number(t);
              onChange(Number.isFinite(n) ? n : t);
            }
          }}
          disabled={disabled}
        />
      );
    case "Text":
      return (
        <Textarea
          id={id}
          rows={4}
          value={asString(value)}
          onChange={(e) => onChange(e.target.value)}
          disabled={disabled}
        />
      );
    case "Selector":
      return (
        <SelectorControl
          id={id}
          options={field.options ?? []}
          value={value}
          onChange={onChange}
          disabled={disabled}
        />
      );
    case "Radio":
      return (
        <RadioControl
          name={id}
          options={field.options ?? []}
          value={value}
          onChange={onChange}
          disabled={disabled}
        />
      );
    case "Switch":
      return (
        <div className={styles.switchRow}>
          <Switch
            id={id}
            checked={asBool(value)}
            onCheckedChange={(v) => onChange(v)}
            disabled={disabled}
            aria-labelledby={`label-${id}`}
          />
        </div>
      );
    case "Boolean":
      return (
        <input
          id={id}
          type="checkbox"
          className={styles.checkbox}
          checked={asBool(value)}
          onChange={(e) => onChange(e.target.checked)}
          disabled={disabled}
        />
      );
    case "Arguments":
      return (
        <ArgumentsControl field={field} value={value} onChange={onChange} disabled={disabled} />
      );
    case "Object":
    default:
      return <ObjectControl id={id} value={value} onChange={onChange} disabled={disabled} />;
  }
}

function asString(v: unknown): string {
  if (v === undefined || v === null) return "";
  if (typeof v === "string") return v;
  if (typeof v === "number" || typeof v === "boolean" || typeof v === "bigint") {
    return String(v);
  }
  // Objects and arrays would stringify to "[object Object]" — preserve a
  // round-trippable JSON shape instead.
  try {
    return JSON.stringify(v);
  } catch {
    return "";
  }
}

function asBool(v: unknown): boolean {
  return v === true;
}

function SelectorControl({
  id,
  options,
  value,
  onChange,
  disabled,
}: {
  id: string;
  options: FormFieldOption[];
  value: unknown;
  onChange: (v: unknown) => void;
  disabled: boolean;
}) {
  // Stringify values for the DOM, but keep the original option value type
  // when reporting back to the parent.
  const stringValue = options.find((o) => asString(o.value) === asString(value))
    ? asString(value)
    : "";
  return (
    <select
      id={id}
      className={styles.select}
      value={stringValue}
      onChange={(e) => {
        const picked = options.find((o) => asString(o.value) === e.target.value);
        onChange(picked ? picked.value : e.target.value);
      }}
      disabled={disabled}
    >
      <option value="" disabled hidden>
        Select…
      </option>
      {options.map((o, i) => (
        <option key={`${asString(o.value)}-${i}`} value={asString(o.value)}>
          {o.text}
        </option>
      ))}
    </select>
  );
}

function RadioControl({
  name,
  options,
  value,
  onChange,
  disabled,
}: {
  name: string;
  options: FormFieldOption[];
  value: unknown;
  onChange: (v: unknown) => void;
  disabled: boolean;
}) {
  return (
    <div className={styles.radioGroup} role="radiogroup">
      {options.map((o, i) => {
        const checked = asString(o.value) === asString(value);
        const rid = `${name}-r${i}`;
        return (
          <label key={rid} className={styles.radioRow} htmlFor={rid}>
            <input
              id={rid}
              type="radio"
              name={name}
              checked={checked}
              onChange={() => onChange(o.value)}
              disabled={disabled}
            />
            {o.text}
          </label>
        );
      })}
    </div>
  );
}

type KvRow = { k: string; v: string };

function isKeyValueArguments(field: FormField): boolean {
  // Heuristic matching the YAML placeholder hint:
  // a 2-element array placeholder ⇒ key/value map.
  const p = field.placeholder;
  return Array.isArray(p) && p.length === 2;
}

function ArgumentsControl({
  field,
  value,
  onChange,
  disabled,
}: {
  field: FormField;
  value: unknown;
  onChange: (v: unknown) => void;
  disabled: boolean;
}) {
  const isMap = isKeyValueArguments(field);

  if (isMap) {
    const placeholders = (field.placeholder as unknown[]).map((p) => String(p));
    const obj =
      value && typeof value === "object" && !Array.isArray(value)
        ? (value as Record<string, unknown>)
        : {};
    const rows: KvRow[] = Object.entries(obj).map(([k, v]) => ({
      k,
      v: asString(v),
    }));

    function commit(nextRows: KvRow[]) {
      const out: Record<string, string> = {};
      for (const r of nextRows) {
        // Skip empty keys so an empty new row doesn't clobber existing ones.
        if (r.k.length === 0) continue;
        out[r.k] = r.v;
      }
      onChange(out);
    }

    return (
      <ArgumentsMapView
        rows={rows}
        placeholders={placeholders}
        onCommit={commit}
        disabled={disabled}
      />
    );
  }

  const ph = typeof field.placeholder === "string" ? field.placeholder : "value";
  const list: string[] = Array.isArray(value) ? (value as unknown[]).map((x) => asString(x)) : [];

  function commit(next: string[]) {
    onChange(next);
  }

  return (
    <div className={styles.argsRows}>
      {list.map((item, i) => (
        <div key={i} className={styles.argsRowSingle}>
          <Input
            value={item}
            onChange={(e) => {
              const n = [...list];
              n[i] = e.target.value;
              commit(n);
            }}
            placeholder={ph}
            disabled={disabled}
          />
          <button
            type="button"
            className={styles.removeBtn}
            onClick={() => commit(list.filter((_, j) => j !== i))}
            disabled={disabled}
            aria-label="Remove row"
          >
            ×
          </button>
        </div>
      ))}
      <button
        type="button"
        className={styles.addBtn}
        onClick={() => commit([...list, ""])}
        disabled={disabled}
      >
        + Add row
      </button>
    </div>
  );
}

function ArgumentsMapView({
  rows,
  placeholders,
  onCommit,
  disabled,
}: {
  rows: KvRow[];
  placeholders: string[];
  onCommit: (rows: KvRow[]) => void;
  disabled: boolean;
}) {
  // Use local row state so an in-progress (empty-key) row is editable without
  // being dropped by the map-collapse during commit.
  const [local, setLocal] = useState<KvRow[]>(rows);
  // When the parent value changes for reasons other than our edits, resync.
  // We diff by the canonical map representation.
  useEffect(() => {
    const canonical = mapFromRows(local);
    const incoming = mapFromRows(rows);
    if (!shallowEqual(canonical, incoming)) {
      setLocal(rows);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [JSON.stringify(rows)]);

  function update(next: KvRow[]) {
    setLocal(next);
    onCommit(next);
  }

  return (
    <div className={styles.argsRows}>
      {local.map((r, i) => (
        <div key={i} className={styles.argsRow}>
          <Input
            value={r.k}
            onChange={(e) => {
              const n = [...local];
              n[i] = { ...n[i]!, k: e.target.value };
              update(n);
            }}
            placeholder={placeholders[0] ?? "key"}
            disabled={disabled}
          />
          <Input
            value={r.v}
            onChange={(e) => {
              const n = [...local];
              n[i] = { ...n[i]!, v: e.target.value };
              update(n);
            }}
            placeholder={placeholders[1] ?? "value"}
            disabled={disabled}
          />
          <button
            type="button"
            className={styles.removeBtn}
            onClick={() => update(local.filter((_, j) => j !== i))}
            disabled={disabled}
            aria-label="Remove row"
          >
            ×
          </button>
        </div>
      ))}
      <button
        type="button"
        className={styles.addBtn}
        onClick={() => update([...local, { k: "", v: "" }])}
        disabled={disabled}
      >
        + Add row
      </button>
    </div>
  );
}

function mapFromRows(rows: KvRow[]): Record<string, string> {
  const out: Record<string, string> = {};
  for (const r of rows) if (r.k.length > 0) out[r.k] = r.v;
  return out;
}

function shallowEqual(a: Record<string, string>, b: Record<string, string>): boolean {
  const ka = Object.keys(a);
  const kb = Object.keys(b);
  if (ka.length !== kb.length) return false;
  for (const k of ka) if (a[k] !== b[k]) return false;
  return true;
}

function ObjectControl({
  id,
  value,
  onChange,
  disabled,
}: {
  id: string;
  value: unknown;
  onChange: (v: unknown) => void;
  disabled: boolean;
}) {
  const initial = useMemo(() => {
    if (value === undefined || value === null) return "";
    try {
      return JSON.stringify(value, null, 2);
    } catch {
      return "";
    }
  }, [value]);
  const [text, setText] = useState<string>(initial);
  const [err, setErr] = useState<string | null>(null);

  // Resync when the underlying value flips identity (e.g. parent reset).
  useEffect(() => {
    setText(initial);
    setErr(null);
  }, [initial]);

  return (
    <>
      <Textarea
        id={id}
        rows={4}
        value={text}
        onChange={(e) => {
          const t = e.target.value;
          setText(t);
          if (t.trim() === "") {
            setErr(null);
            onChange(undefined);
            return;
          }
          try {
            const parsed = JSON.parse(t) as unknown;
            setErr(null);
            onChange(parsed);
          } catch (ex) {
            setErr(ex instanceof Error ? ex.message : "invalid JSON");
          }
        }}
        invalid={!!err}
        className={styles.jsonTextarea}
        disabled={disabled}
      />
      {err ? <span className={styles.jsonError}>{err}</span> : null}
    </>
  );
}
