import { useState } from "react";
import { useWatch, type Control, type UseFormSetValue } from "react-hook-form";
import { timeConstraintsError } from "@/lib/timeconstraints/validate";
import { CollapsibleSection } from "@/shared/ui/CollapsibleSection";
import { ConditionPreview } from "@/shared/ui/ConditionPreview";
import { Button } from "@/shared/ui/Button";
import { Switch } from "@/shared/ui/Switch";
import { Textarea } from "@/shared/ui/Textarea";
import { Input } from "@/shared/ui/Input";
import { TimeConstraintsCell } from "@/shared/ui/TimeConstraintsCell";
import { TimeConstraintsEditor } from "@/shared/ui/TimeConstraintsEditor";
import { ConditionEditor } from "@/shared/condition/ConditionEditor";
import type { Condition } from "@/lib/condition/types";
import type { TimeConstraintsGroup } from "@/lib/timeconstraints/types";
import { DiffSection } from "@/shared/ui/DiffSection";
import {
  EditorAbort,
  EditorDrawer,
  useFieldInvalid,
  type EditorBodyProps,
} from "@/shared/forms/EditorDrawer";
import { Snoozes } from "./api";
import { parseDuration } from "./duration";
import type { Snooze } from "./types";
import styles from "./SnoozeEditor.module.css";

// Preset "silence for" durations, in seconds. Mirrors the common operator
// shortcuts (an hour, half a shift, a day, a week).
const SILENCE_PRESETS: { label: string; seconds: number }[] = [
  { label: "1h", seconds: 3600 },
  { label: "4h", seconds: 14400 },
  { label: "24h", seconds: 86400 },
  { label: "7d", seconds: 604800 },
];

type FormShape = {
  name: string;
  comment: string;
  enabled: boolean;
  condition: Condition;
  time_constraints: TimeConstraintsGroup;
  discard: boolean;
};

const EMPTY_FORM: FormShape = {
  name: "",
  comment: "",
  enabled: true,
  condition: { type: "ALWAYS_TRUE" },
  time_constraints: {},
  discard: false,
};

export type SnoozeEditorProps = {
  uid: string | undefined;
  onClose: () => void;
};

export function SnoozeEditor({ uid, onClose }: SnoozeEditorProps) {
  const isCreate = uid === undefined || uid === "";
  const get = Snoozes.useGet(isCreate ? undefined : uid);
  const create = Snoozes.useCreate();
  const update = Snoozes.useUpdate();
  // Set in formToBody before throwing EditorAbort; read by the body to render
  // the inline message (mirrors WidgetEditor's jsonError pattern).
  const [tcError, setTcError] = useState<string | null>(null);

  return (
    <EditorDrawer<FormShape, Snooze>
      uid={uid}
      onClose={onClose}
      get={get}
      create={create}
      update={update}
      emptyForm={EMPTY_FORM}
      recordToForm={(s) => ({
        name: s.name ?? "",
        comment: s.comment ?? "",
        enabled: s.enabled ?? true,
        condition: s.condition ?? { type: "ALWAYS_TRUE" },
        time_constraints: s.time_constraints ?? {},
        discard: s.discard ?? false,
      })}
      formToBody={(form) => {
        setTcError(null);
        // Block an incomplete time window (e.g. an unfilled "Add range"), which
        // would save a snooze that never matches yet reads as "always on".
        const tcErr = timeConstraintsError(form.time_constraints);
        if (tcErr) {
          setTcError(tcErr);
          throw new EditorAbort();
        }
        const hasTimeConstraints =
          (form.time_constraints.datetime?.length ?? 0) > 0 ||
          (form.time_constraints.time?.length ?? 0) > 0 ||
          (form.time_constraints.weekdays?.length ?? 0) > 0;
        const body: Snooze = {
          name: form.name,
          ...(form.comment ? { comment: form.comment } : {}),
          enabled: form.enabled,
          condition: form.condition,
          ...(hasTimeConstraints ? { time_constraints: form.time_constraints } : {}),
          ...(form.discard ? { discard: true } : {}),
        };
        return body;
      }}
      title={(c) => (c ? "New snooze" : "Edit snooze")}
      titleToolbar={({ control, setValue }) => (
        <SnoozeEnabledToggle control={control} setValue={setValue} />
      )}
      footerStart={({ control }) => (
        <SnoozeDiff control={control} original={isCreate ? undefined : get.data} />
      )}
      successMessage={{ create: "Snooze created", update: "Snooze saved" }}
      formId="snooze-form"
      formClassName={styles.stack}
    >
      {(body) => <SnoozeFields {...body} tcError={tcError} />}
    </EditorDrawer>
  );
}

/** Enabled switch scoped to its own `enabled` subscription — toggling
 *  re-renders only this slot, not the whole drawer. */
function SnoozeEnabledToggle({
  control,
  setValue,
}: {
  control: Control<FormShape>;
  setValue: UseFormSetValue<FormShape>;
}) {
  const enabled = useWatch({ control, name: "enabled" });
  return (
    <>
      <Switch
        checked={enabled}
        onCheckedChange={(v) => setValue("enabled", v, { shouldDirty: true })}
        aria-label="Enabled"
      />
      <span>{enabled ? "Enabled" : "Disabled"}</span>
    </>
  );
}

/** Diff scoped to its own subscriptions, mirroring RuleEditor's RuleDiff. */
function SnoozeDiff({
  control,
  original,
}: {
  control: Control<FormShape>;
  original: Snooze | undefined;
}) {
  const name = useWatch({ control, name: "name" });
  const comment = useWatch({ control, name: "comment" });
  const enabled = useWatch({ control, name: "enabled" });
  const condition = useWatch({ control, name: "condition" });
  const discard = useWatch({ control, name: "discard" });
  const timeConstraints = useWatch({ control, name: "time_constraints" });
  // Mirror formToBody's conditional inclusion so the diff reflects time-window
  // edits (previously omitted entirely, so a snooze schedule change showed no diff).
  const hasTimeConstraints =
    (timeConstraints.datetime?.length ?? 0) > 0 ||
    (timeConstraints.time?.length ?? 0) > 0 ||
    (timeConstraints.weekdays?.length ?? 0) > 0;
  const projected: Snooze = {
    name,
    ...(comment ? { comment } : {}),
    enabled,
    condition,
    ...(hasTimeConstraints ? { time_constraints: timeConstraints } : {}),
    ...(discard ? { discard: true } : {}),
  };
  return <DiffSection original={original} current={projected} />;
}

function SnoozeFields({
  control,
  register,
  setValue,
  tcError,
}: EditorBodyProps<FormShape> & { tcError: string | null }) {
  const nameInvalid = useFieldInvalid(control, "name");
  const condition = useWatch({ control, name: "condition" });
  const tc = useWatch({ control, name: "time_constraints" });
  const discard = useWatch({ control, name: "discard" });

  return (
    <>
      <section className={styles.section}>
        <h3 className={styles.sectionTitle}>Identity</h3>
        <div className={styles.field}>
          <label className={styles.label} htmlFor="snooze-name">
            Name
          </label>
          <Input
            id="snooze-name"
            {...register("name")}
            invalid={nameInvalid}
            placeholder="e.g. quiet-friday-night"
          />
        </div>
        <span className={styles.row}>
          <Switch
            checked={discard}
            onCheckedChange={(v) => setValue("discard", v, { shouldDirty: true })}
            aria-label="Discard"
          />
          <span>Discard matching alerts</span>
        </span>
      </section>
      <section className={styles.section}>
        <h3 className={styles.sectionTitle}>Condition</h3>
        <ConditionEditor
          value={condition}
          onChange={(c) => setValue("condition", c, { shouldDirty: true })}
          plugin="record"
        />
        <div style={{ marginTop: "var(--space-2)" }}>
          <ConditionPreview condition={condition} />
        </div>
      </section>
      <SilenceFor
        onApply={(durationSeconds) => {
          const now = new Date();
          const until = new Date(now.getTime() + durationSeconds * 1000);
          setValue(
            "time_constraints",
            {
              ...tc,
              datetime: [{ from: now.toISOString(), until: until.toISOString() }],
            },
            { shouldDirty: true },
          );
        }}
      />
      <CollapsibleSection
        title="Time constraints"
        summary={<TimeConstraintsCell value={tc} />}
        defaultOpen
      >
        <TimeConstraintsEditor
          value={tc}
          onChange={(g) => setValue("time_constraints", g, { shouldDirty: true })}
        />
        {tcError ? (
          <span
            role="alert"
            style={{ color: "var(--severity-critical)", fontSize: "var(--text-xs)" }}
          >
            {tcError}
          </span>
        ) : null}
      </CollapsibleSection>
      <div className={styles.field}>
        <label className={styles.label} htmlFor="snooze-comment">
          Comment
        </label>
        <Textarea
          id="snooze-comment"
          {...register("comment")}
          rows={2}
          placeholder="Optional description"
        />
      </div>
    </>
  );
}

/** "Silence for…" duration shortcut. Preset buttons plus a free-text field
 *  (parseDuration-validated) that set a single absolute datetime window from
 *  now, so operators can quiet a host until morning without the date-picker.
 *  Invalid free text shows a message and does NOT mutate the form. */
function SilenceFor({ onApply }: { onApply: (durationSeconds: number) => void }) {
  const [text, setText] = useState("");
  const [error, setError] = useState<string | null>(null);

  const applyFreeText = () => {
    const seconds = parseDuration(text);
    if (seconds === null) {
      setError('Invalid duration — try "2h", "30m", or "1d12h".');
      return;
    }
    setError(null);
    setText("");
    onApply(seconds);
  };

  return (
    <section className={styles.section}>
      <h3 className={styles.sectionTitle}>Silence for…</h3>
      <div className={styles.row}>
        {SILENCE_PRESETS.map((p) => (
          <Button
            key={p.label}
            size="sm"
            variant="secondary"
            onClick={() => {
              setError(null);
              onApply(p.seconds);
            }}
          >
            {p.label}
          </Button>
        ))}
        <Input
          aria-label="Silence for (custom duration)"
          placeholder="e.g. 2h30m"
          size="sm"
          value={text}
          invalid={error !== null}
          onChange={(e) => {
            setText(e.target.value);
            if (error) setError(null);
          }}
          onKeyDown={(e) => {
            if (e.key === "Enter") {
              e.preventDefault();
              applyFreeText();
            }
          }}
        />
        <Button size="sm" variant="secondary" onClick={applyFreeText}>
          Apply
        </Button>
      </div>
      {error ? (
        <span role="alert" style={{ color: "var(--severity-error)", fontSize: "var(--text-xs)" }}>
          {error}
        </span>
      ) : null}
    </section>
  );
}
