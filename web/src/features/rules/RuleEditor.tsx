import { useMemo, type ReactNode } from "react";
import {
  useWatch,
  useFormState,
  type Control,
  type UseFormRegister,
  type UseFormSetValue,
} from "react-hook-form";
import type { UseMutationResult } from "@tanstack/react-query";
import { Button } from "@/shared/ui/Button";
import { ConditionPreview } from "@/shared/ui/ConditionPreview";
import { DurationInput } from "@/shared/ui/DurationInput";
import { Input } from "@/shared/ui/Input";
import { MultiCombobox } from "@/shared/ui/MultiCombobox";
import { Switch } from "@/shared/ui/Switch";
import { Textarea } from "@/shared/ui/Textarea";
import { ConditionEditor } from "@/shared/condition/ConditionEditor";
import { ModificationList } from "@/shared/modifications/ModificationList";
import type { ApiError } from "@/lib/api/client";
import type { Condition } from "@/lib/condition/types";
import type { Modification } from "@/shared/modifications/types";
import { modificationsFromWire, modificationsToWire } from "@/shared/modifications/wire";
import { DiffSection } from "@/shared/ui/DiffSection";
import { EditorDrawer, EditorAbort, type EditorBodyProps } from "@/shared/forms/EditorDrawer";
import { Rules, AggregateRules } from "./api";
import type { AggregateRule, Rule } from "./types";
import { throttleFromWire, throttleToWire, type ThrottleOverride } from "./throttle";
import styles from "./RuleEditor.module.css";

type FormShape = {
  name: string;
  comment: string;
  enabled: boolean;
  condition: Condition;
  modifications: Modification[];
  fields: string[];
  watch: string[];
  throttleDefault: number;
  throttleOverrides: ThrottleOverride[];
};

const EMPTY_FORM: FormShape = {
  name: "",
  comment: "",
  enabled: true,
  condition: { type: "ALWAYS_TRUE" },
  modifications: [],
  fields: [],
  watch: [],
  throttleDefault: 0,
  throttleOverrides: [],
};

type RuleBody = Rule & Partial<AggregateRule>;

/** RuleInsertion captures where a brand-new rule should land in the tree
 *  plus any sibling re-numbering needed to make room. Pages computing
 *  insertion targets (per-row "Add above / Add below / Add as child")
 *  pass this to the editor; the editor applies the sibling shifts first
 *  and then creates the new rule with the matching parents + tree_order.
 *
 *  Only meaningful for the rule plugin — AggregateRules don't carry tree
 *  position. When set with plugin="aggregaterule" the editor ignores it. */
export type RuleInsertion = {
  parents: string[];
  tree_order: number;
  /** Existing siblings whose tree_order has to bump up to make room for
   *  the new rule. Empty list = no shifts needed (e.g. appending as the
   *  last child of an existing parent). */
  siblingPatches: Array<{ uid: string; tree_order: number }>;
};

export type RuleEditorProps = {
  plugin: "rule" | "aggregaterule";
  uid: string | undefined;
  onClose: () => void;
  /** When set (and we're in create mode), the new rule lands at this
   *  position; sibling shifts run first via update.mutateAsync. */
  insertion?: RuleInsertion;
};

export function RuleEditor({ plugin, uid, onClose, insertion }: RuleEditorProps) {
  const isCreate = uid === undefined || uid === "";
  const isAggregate = plugin === "aggregaterule";
  const resource = isAggregate ? AggregateRules : Rules;
  const get = resource.useGet(isCreate ? undefined : uid);
  const create = resource.useCreate();
  const update = resource.useUpdate();

  const labelPlugin = isAggregate ? "aggregate rule" : "rule";

  // When the host passed an insertion target, the new rule's tree position is
  // baked into the POST body — and existing siblings get re-numbered first to
  // make room. Aggregate rules don't carry tree position, so the hint is
  // ignored there.
  const activeInsertion: RuleInsertion | undefined =
    isCreate && !isAggregate ? insertion : undefined;

  // The insertion "shift siblings then create" sequence is expressed as a
  // create-like mutation wrapper: its mutateAsync PATCHes the colliding
  // siblings (sequentially — parallel PATCHes on the same parent occasionally
  // race in the SQL backend's optimistic-update path) and then POSTs the new
  // rule. The frame calls it exactly like the plain create, so no frame change
  // is needed. Without an active insertion this is just the raw create.
  const createForFrame = useMemo<
    UseMutationResult<AggregateRule, ApiError, Partial<AggregateRule>>
  >(() => {
    if (!activeInsertion || activeInsertion.siblingPatches.length === 0) {
      return create as UseMutationResult<AggregateRule, ApiError, Partial<AggregateRule>>;
    }
    const base = create as UseMutationResult<AggregateRule, ApiError, Partial<AggregateRule>>;
    const wrappedMutateAsync = (async (body: Partial<AggregateRule>) => {
      for (const p of activeInsertion.siblingPatches) {
        await update.mutateAsync({ uid: p.uid, body: { tree_order: p.tree_order } });
      }
      return base.mutateAsync(body);
    }) as typeof base.mutateAsync;
    return { ...base, mutateAsync: wrappedMutateAsync };
  }, [create, update, activeInsertion]);

  return (
    <EditorDrawer<FormShape, AggregateRule>
      uid={uid}
      onClose={onClose}
      get={get}
      create={createForFrame}
      update={update}
      emptyForm={EMPTY_FORM}
      recordToForm={(agg) => {
        const t = throttleFromWire(agg.throttle);
        return {
          name: agg.name ?? "",
          comment: agg.comment ?? "",
          enabled: agg.enabled ?? true,
          condition: agg.condition ?? { type: "ALWAYS_TRUE" },
          modifications: modificationsFromWire(agg.modifications),
          fields: agg.fields ?? [],
          watch: agg.watch ?? [],
          throttleDefault: t.defaultSeconds,
          throttleOverrides: t.overrides,
        };
      }}
      formToBody={(form) => {
        // `register` is used without validation rules; enforce a non-empty
        // name at submit. The inline invalid border (RuleNameField) renders
        // via isSubmitted; abort silently so no round-trip closes the drawer.
        if (!form.name.trim()) throw new EditorAbort();
        const throttleWire = throttleToWire({
          defaultSeconds: form.throttleDefault,
          overrides: form.throttleOverrides,
        });
        const includeThrottle =
          isAggregate &&
          (typeof throttleWire === "number"
            ? throttleWire > 0
            : Object.keys(throttleWire).length > 0);
        const body: RuleBody = {
          name: form.name,
          ...(form.comment ? { comment: form.comment } : {}),
          enabled: form.enabled,
          condition: form.condition,
          ...(form.modifications.length > 0
            ? { modifications: modificationsToWire(form.modifications) }
            : {}),
          ...(isAggregate && form.fields.length > 0 ? { fields: form.fields } : {}),
          ...(isAggregate && form.watch.length > 0 ? { watch: form.watch } : {}),
          ...(includeThrottle ? { throttle: throttleWire } : {}),
          ...(activeInsertion
            ? { parents: activeInsertion.parents, tree_order: activeInsertion.tree_order }
            : {}),
        };
        return body;
      }}
      title={(c): ReactNode =>
        c
          ? insertion && plugin === "rule"
            ? `New ${labelPlugin} · ${insertion.parents.length > 0 ? "child" : "root"} at position ${insertion.tree_order + 1}`
            : `New ${labelPlugin}`
          : `Edit ${labelPlugin}`
      }
      titleToolbar={({ control, setValue }) => (
        <RuleEnabledToggle control={control} setValue={setValue} />
      )}
      footerStart={({ control }) => (
        <RuleDiff control={control} original={isCreate ? undefined : get.data} />
      )}
      successMessage={{ create: "Created", update: "Saved" }}
      formId="rule-form"
      formClassName={styles.stack}
    >
      {(body) => <RuleFields {...body} isAggregate={isAggregate} />}
    </EditorDrawer>
  );
}

/** Enabled switch scoped to its own subscription — toggling re-renders only
 *  this slot, never ConditionEditor / ModificationList / the diff. */
function RuleEnabledToggle({
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

function RuleFields({
  control,
  register,
  setValue,
  isAggregate,
}: EditorBodyProps<FormShape> & { isAggregate: boolean }) {
  // Scoped subscriptions: each useWatch re-renders only this component when
  // its field changes. Name/comment are intentionally NOT watched here —
  // they're uncontrolled `register` inputs, so typing in them must not
  // re-render the drawer (and thus ConditionEditor / ModificationList / the
  // diff). The Name field owns its own invalid state in a child below.
  const condition = useWatch({ control, name: "condition" });
  const modifications = useWatch({ control, name: "modifications" });
  const fields = useWatch({ control, name: "fields" });
  const watchFields = useWatch({ control, name: "watch" });
  const throttleDefault = useWatch({ control, name: "throttleDefault" });
  const throttleOverrides = useWatch({ control, name: "throttleOverrides" });

  return (
    <>
      <section className={styles.section}>
        <h3 className={styles.sectionTitle}>Identity</h3>
        <div className={styles.field}>
          <label className={styles.label} htmlFor="rule-name">
            Name
          </label>
          <RuleNameField control={control} register={register} />
        </div>
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
      <section className={styles.section}>
        <h3 className={styles.sectionTitle}>Modifications</h3>
        <ModificationList
          value={modifications}
          onChange={(m) => setValue("modifications", m, { shouldDirty: true })}
        />
      </section>
      {isAggregate ? (
        <section className={styles.section}>
          <h3 className={styles.sectionTitle}>Aggregation</h3>
          <div className={styles.field}>
            <span className={styles.label}>Fields (group key)</span>
            <MultiCombobox
              aria-label="Aggregation fields"
              placeholder="e.g. host, source"
              options={[]}
              value={fields}
              onChange={(next) => setValue("fields", next, { shouldDirty: true })}
              allowCustom
            />
          </div>
          <div className={styles.field}>
            <span className={styles.label}>Watch (fields tracked for changes)</span>
            <MultiCombobox
              aria-label="Watch fields"
              placeholder="e.g. severity, state"
              options={[]}
              value={watchFields}
              onChange={(next) => setValue("watch", next, { shouldDirty: true })}
              allowCustom
            />
          </div>
          <div className={styles.field}>
            <label className={styles.label} htmlFor="rule-throttle">
              Throttle — default (0 = unlimited)
            </label>
            <DurationInput
              id="rule-throttle"
              value={throttleDefault}
              onChange={(n) => setValue("throttleDefault", n, { shouldDirty: true })}
            />
            <div style={{ marginTop: "var(--space-2)" }}>
              <span className={styles.label}>Overrides</span>
              <p
                style={{
                  margin: "var(--space-1) 0",
                  color: "var(--text-muted)",
                  fontSize: "var(--font-sm)",
                }}
              >
                {watchFields.length > 0
                  ? `Matched against watch values (${watchFields.join(", ")}) — first match wins.`
                  : "Add fields to Watch above so overrides can match a value."}
              </p>
              {throttleOverrides.map((row, i) => (
                <div
                  key={i}
                  style={{
                    display: "flex",
                    gap: "var(--space-2)",
                    alignItems: "center",
                    marginBottom: "var(--space-1)",
                  }}
                >
                  <Input
                    aria-label={`Override value ${i + 1}`}
                    placeholder="e.g. emergency"
                    value={row.value}
                    onChange={(e) => {
                      const next = throttleOverrides.slice();
                      next[i] = { ...next[i], value: e.target.value } as ThrottleOverride;
                      setValue("throttleOverrides", next, { shouldDirty: true });
                    }}
                  />
                  <DurationInput
                    aria-label={`Override duration ${i + 1}`}
                    value={row.seconds}
                    onChange={(n) => {
                      const next = throttleOverrides.slice();
                      next[i] = { ...next[i], seconds: n } as ThrottleOverride;
                      setValue("throttleOverrides", next, { shouldDirty: true });
                    }}
                  />
                  <Button
                    type="button"
                    variant="ghost"
                    aria-label={`Remove override ${i + 1}`}
                    onClick={() =>
                      setValue(
                        "throttleOverrides",
                        throttleOverrides.filter((_, j) => j !== i),
                        { shouldDirty: true },
                      )
                    }
                  >
                    ×
                  </Button>
                </div>
              ))}
              <Button
                type="button"
                variant="ghost"
                onClick={() =>
                  setValue("throttleOverrides", [...throttleOverrides, { value: "", seconds: 0 }], {
                    shouldDirty: true,
                  })
                }
              >
                + Add override
              </Button>
            </div>
          </div>
        </section>
      ) : null}
      <div className={styles.field}>
        <label className={styles.label} htmlFor="rule-comment">
          Comment
        </label>
        <Textarea
          id="rule-comment"
          {...register("comment")}
          rows={2}
          placeholder="Optional description"
        />
      </div>
    </>
  );
}

/** Name input scoped to its own subscriptions. Typing here re-renders only
 *  this component (and the invalid border after a failed submit), never the
 *  whole drawer — `name` is otherwise read only at submit time. */
function RuleNameField({
  control,
  register,
}: {
  control: Control<FormShape>;
  register: UseFormRegister<FormShape>;
}) {
  const name = useWatch({ control, name: "name" });
  const { isSubmitted } = useFormState({ control });
  const invalid = isSubmitted && !name.trim();
  return (
    <Input
      id="rule-name"
      {...register("name")}
      invalid={invalid}
      placeholder="e.g. tag-prod-hosts"
    />
  );
}

/** Diff scoped to its own subscriptions. Building `projected` here (instead
 *  of in the parent) keeps name/comment keystrokes from re-rendering
 *  ConditionEditor / ConditionPreview / ModificationList. The projected
 *  object is memoized so DiffSection's `current` only changes identity when
 *  a field that actually feeds the wire payload changes. */
function RuleDiff({ control, original }: { control: Control<FormShape>; original: unknown }) {
  const name = useWatch({ control, name: "name" });
  const comment = useWatch({ control, name: "comment" });
  const enabled = useWatch({ control, name: "enabled" });
  const condition = useWatch({ control, name: "condition" });
  const modifications = useWatch({ control, name: "modifications" });
  // The Diff section compares against the server payload, so the projected
  // value must use the same wire shape (positional modifications).
  const projected = useMemo<Rule>(
    () => ({
      name,
      ...(comment ? { comment } : {}),
      enabled,
      condition,
      ...(modifications.length > 0 ? { modifications: modificationsToWire(modifications) } : {}),
    }),
    [name, comment, enabled, condition, modifications],
  );
  return <DiffSection original={original} current={projected} />;
}
