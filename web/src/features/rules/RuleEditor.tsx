import { useEffect, useMemo, useState } from "react";
import {
  useForm,
  useFormState,
  useWatch,
  type Control,
  type UseFormRegister,
} from "react-hook-form";
import { Button } from "@/shared/ui/Button";
import { ConditionPreview } from "@/shared/ui/ConditionPreview";
import { Dialog, DialogBody, DialogContent, DialogFooter, DialogTitle } from "@/shared/ui/Dialog";
import { Drawer, DrawerBody, DrawerContent, DrawerFooter, DrawerTitle } from "@/shared/ui/Drawer";
import { DurationInput } from "@/shared/ui/DurationInput";
import { Input } from "@/shared/ui/Input";
import { MultiCombobox } from "@/shared/ui/MultiCombobox";
import { Spinner } from "@/shared/ui/Spinner";
import { Switch } from "@/shared/ui/Switch";
import { Textarea } from "@/shared/ui/Textarea";
import { toast } from "@/shared/ui/toast/useToast";
import { ConditionEditor } from "@/shared/condition/ConditionEditor";
import { ModificationList } from "@/shared/modifications/ModificationList";
import { ApiError } from "@/lib/api/client";
import type { Condition } from "@/lib/condition/types";
import type { Modification } from "@/shared/modifications/types";
import { modificationsFromWire, modificationsToWire } from "@/shared/modifications/wire";
import { DiffSection } from "@/shared/ui/DiffSection";
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
  const resource = plugin === "rule" ? Rules : AggregateRules;
  const existing = resource.useGet(isCreate ? undefined : uid);
  const create = resource.useCreate();
  const update = resource.useUpdate();

  const { register, handleSubmit, reset, control, setValue } = useForm<FormShape>({
    defaultValues: EMPTY_FORM,
  });

  // Subscribe to the form's dirty flag so closing the drawer with unsaved
  // edits prompts a confirm step instead of silently discarding them.
  // `isDirty` toggles false→true on the first edit and then stays true, so
  // this subscription costs at most one extra drawer re-render per editing
  // session — it does not re-render per keystroke (name/comment remain
  // uncontrolled `register` inputs; see the useWatch note below).
  const { isDirty } = useFormState({ control });
  const [confirmDiscardOpen, setConfirmDiscardOpen] = useState(false);

  // The save path calls onClose() directly (see onSubmit) and never routes
  // through here, so a successful Create/Save closes without the guard. Only
  // the user-initiated close affordances (Escape / X / Cancel / overlay) hit
  // requestClose, and only a dirty form raises the confirm.
  function requestClose() {
    if (isDirty) {
      setConfirmDiscardOpen(true);
      return;
    }
    onClose();
  }

  useEffect(() => {
    if (isCreate) {
      reset(EMPTY_FORM);
      return;
    }
    if (existing.data) {
      const agg = existing.data as AggregateRule;
      const t = throttleFromWire(agg.throttle);
      reset({
        name: existing.data.name ?? "",
        comment: existing.data.comment ?? "",
        enabled: existing.data.enabled ?? true,
        condition: existing.data.condition ?? { type: "ALWAYS_TRUE" },
        modifications: modificationsFromWire(existing.data.modifications),
        fields: agg.fields ?? [],
        watch: agg.watch ?? [],
        throttleDefault: t.defaultSeconds,
        throttleOverrides: t.overrides,
      });
    }
  }, [existing.data, isCreate, reset]);

  const [submitting, setSubmitting] = useState(false);

  async function onSubmit(form: FormShape) {
    if (!form.name.trim()) {
      // RHF's `register` is used without rules; enforce a non-empty name at
      // submit so the inline invalid state has time to render before any
      // network round-trip closes the drawer.
      toast.error("Name is required");
      return;
    }
    setSubmitting(true);
    try {
      const isAggregate = plugin === "aggregaterule";
      // When the host passed an insertion target (per-row "Add above /
      // below / as child"), the new rule gets its tree position baked into
      // the POST body — and existing siblings get re-numbered first to
      // make room. Aggregate rules don't carry tree position, so the
      // insertion hint is ignored there.
      const activeInsertion: RuleInsertion | undefined =
        isCreate && !isAggregate ? insertion : undefined;
      const throttleWire = throttleToWire({
        defaultSeconds: form.throttleDefault,
        overrides: form.throttleOverrides,
      });
      const includeThrottle =
        isAggregate &&
        (typeof throttleWire === "number"
          ? throttleWire > 0
          : Object.keys(throttleWire).length > 0);
      const body: Rule & Partial<AggregateRule> = {
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
      if (isCreate) {
        if (activeInsertion && activeInsertion.siblingPatches.length > 0) {
          // Shift colliding siblings BEFORE the create so the new rule's
          // target tree_order is unambiguous. Sequential on purpose:
          // parallel PATCHes on the same parent occasionally race in the
          // SQL backend's optimistic-update path.
          for (const p of activeInsertion.siblingPatches) {
            await update.mutateAsync({ uid: p.uid, body: { tree_order: p.tree_order } });
          }
        }
        await create.mutateAsync(body);
        toast.success("Created");
      } else {
        await update.mutateAsync({ uid, body });
        toast.success("Saved");
      }
      onClose();
    } catch (e) {
      toast.error(e instanceof ApiError ? e.detail : "Save failed");
    } finally {
      setSubmitting(false);
    }
  }

  // Scoped subscriptions: each useWatch re-renders only this component when
  // its field changes. Name/comment are intentionally NOT watched here —
  // they're uncontrolled `register` inputs, so typing in them must not
  // re-render the drawer (and thus ConditionEditor / ModificationList / the
  // diff). The Name field owns its own invalid state in a child below.
  const condition = useWatch({ control, name: "condition" });
  const modifications = useWatch({ control, name: "modifications" });
  const enabled = useWatch({ control, name: "enabled" });
  const fields = useWatch({ control, name: "fields" });
  const watchFields = useWatch({ control, name: "watch" });
  const throttleDefault = useWatch({ control, name: "throttleDefault" });
  const throttleOverrides = useWatch({ control, name: "throttleOverrides" });
  const isAggregate = plugin === "aggregaterule";
  const labelPlugin = plugin === "rule" ? "rule" : "aggregate rule";

  return (
    <Drawer
      open
      onOpenChange={(o) => {
        if (!o) requestClose();
      }}
    >
      <DrawerContent>
        <DrawerTitle
          toolbar={
            <>
              <Switch
                checked={enabled}
                onCheckedChange={(v) => setValue("enabled", v, { shouldDirty: true })}
                aria-label="Enabled"
              />
              <span>{enabled ? "Enabled" : "Disabled"}</span>
            </>
          }
        >
          {isCreate
            ? insertion && plugin === "rule"
              ? `New ${labelPlugin} · ${insertion.parents.length > 0 ? "child" : "root"} at position ${insertion.tree_order + 1}`
              : `New ${labelPlugin}`
            : `Edit ${labelPlugin}`}
        </DrawerTitle>
        <DrawerBody>
          {!isCreate && existing.isPending ? (
            <div
              style={{
                display: "flex",
                justifyContent: "center",
                padding: "var(--space-5)",
              }}
            >
              <Spinner size={20} />
            </div>
          ) : (
            <form
              id="rule-form"
              className={styles.stack}
              onSubmit={(e) => void handleSubmit(onSubmit)(e)}
            >
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
                          setValue(
                            "throttleOverrides",
                            [...throttleOverrides, { value: "", seconds: 0 }],
                            { shouldDirty: true },
                          )
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
            </form>
          )}
        </DrawerBody>
        <DrawerFooter>
          <div style={{ flex: 1 }}>
            <RuleDiff control={control} original={isCreate ? undefined : existing.data} />
          </div>
          <Button variant="ghost" onClick={requestClose}>
            Cancel
          </Button>
          <Button
            type="submit"
            form="rule-form"
            variant="primary"
            loading={submitting}
            disabled={submitting}
          >
            {isCreate ? "Create" : "Save"}
          </Button>
        </DrawerFooter>
      </DrawerContent>
      <Dialog open={confirmDiscardOpen} onOpenChange={setConfirmDiscardOpen}>
        <DialogContent>
          <DialogTitle>Discard changes?</DialogTitle>
          <DialogBody>You have unsaved changes. Closing now will discard them.</DialogBody>
          <DialogFooter>
            <Button variant="secondary" onClick={() => setConfirmDiscardOpen(false)}>
              Keep editing
            </Button>
            <Button
              variant="danger"
              onClick={() => {
                setConfirmDiscardOpen(false);
                onClose();
              }}
            >
              Discard
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </Drawer>
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
