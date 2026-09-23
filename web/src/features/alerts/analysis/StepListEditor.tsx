// An ordered, reorderable list of remediation (or rollback) steps.
//
// One component serves both `remediation_plan.steps` and
// `remediation_plan.rollback` — same row, same rules, different path prefix
// and different floor (a plan needs one step; a rollback may be empty). That
// is why every lookup here is by runtime path rather than typed accessor.
//
// Reordering is offered TWICE on purpose. Drag is the fast path for a mouse;
// the Move up / Move down buttons are the path everyone else takes, and they
// are real buttons rather than a reliance on dnd-kit's keyboard sensor, which
// only works once the drag handle has been found and focused. The dnd-kit
// sensors are mounted inside this component (never app-wide): the editor
// lives in a Radix tab panel that unmounts when the tab changes, so the
// listeners go with it.
import { useEffect, useId, useRef } from "react";
import {
  DndContext,
  KeyboardSensor,
  PointerSensor,
  closestCenter,
  useSensor,
  useSensors,
  type DragEndEvent,
} from "@dnd-kit/core";
import {
  SortableContext,
  sortableKeyboardCoordinates,
  useSortable,
  verticalListSortingStrategy,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import {
  useFieldArray,
  useWatch,
  type Control,
  type FieldErrors,
  type UseFormRegister,
  type UseFormSetValue,
} from "react-hook-form";
import { Button } from "@/shared/ui/Button";
import { IconButton } from "@/shared/ui/IconButton";
import { Select, SelectContent, SelectItem, SelectTrigger } from "@/shared/ui/Select";
import { Textarea } from "@/shared/ui/Textarea";
import { CharCounter } from "./CharCounter";
import { RISK_LEVELS, riskLabel } from "./enums";
import { STEP_WHENS, stepWhenLabel } from "./verdict";
import { describeFieldError, errorAt } from "./fieldErrors";
import { ANALYSIS_LIMITS, emptyAnalysisStep, type AnalysisForm } from "./schema";
import styles from "./editor.module.css";
import own from "./StepListEditor.module.css";

/** The two paths this editor is mounted on. */
export type StepListName = "remediation_plan.steps" | "remediation_plan.rollback";

type StepField = "action" | "command" | "risk" | "when";

/**
 * Radix Select cannot hold "" as an item value, and "not stated" is a real
 * choice an author makes (it is the default) — so it rides a sentinel that
 * never reaches the form.
 */
const WHEN_UNSET = "unset";

/**
 * A concrete react-hook-form path into one step. Spelled as a template type
 * (rather than the broad `FieldPath`) so `setValue` still knows the field's
 * value type.
 */
type StepPath<F extends StepField> =
  | `remediation_plan.steps.${number}.${F}`
  | `remediation_plan.rollback.${number}.${F}`;

function stepPath<F extends StepField>(name: StepListName, index: number, field: F): StepPath<F> {
  return `${name}.${index}.${field}`;
}

/**
 * Grow a command box to its content. A remediation command is often two or
 * three lines and scrolling a two-row box to check what you are about to paste
 * into a terminal is how a wrong command gets run.
 */
function autoGrow(el: HTMLTextAreaElement | null): void {
  if (!el) return;
  el.style.height = "auto";
  el.style.height = `${el.scrollHeight}px`;
}

export type StepListEditorProps = {
  control: Control<AnalysisForm>;
  register: UseFormRegister<AnalysisForm>;
  setValue: UseFormSetValue<AnalysisForm>;
  errors: FieldErrors<AnalysisForm>;
  /** Which of the two lists this instance edits. */
  name: StepListName;
  /** Heading above the list ("Steps", "Rollback"). */
  heading: string;
  /** Smallest list the server accepts: 1 for steps, 0 for rollback. */
  min: number;
  /** Largest list the server accepts. */
  max: number;
  /** Label of the append button — the two lists must not both say "Add step". */
  addLabel: string;
  /** Singular noun used in every per-row control label ("step"). */
  noun: string;
  /** Shown as the disabled Remove button's tooltip once the list is at `min`. */
  removeDisabledHint: string;
  /** Re-run the resolver on every edit once the author has tried to save. */
  validateOnChange: boolean;
  /**
   * Offer the per-step "When" (now / follow-up). On for the plan's steps; off
   * for the rollback, whose steps have no "now" of their own — they run when
   * a step went wrong.
   */
  withWhen?: boolean;
};

export function StepListEditor(props: StepListEditorProps) {
  const {
    control,
    register,
    setValue,
    errors,
    name,
    heading,
    min,
    max,
    addLabel,
    noun,
    removeDisabledHint,
    validateOnChange,
    withWhen = false,
  } = props;

  const baseId = useId();
  const { fields, append, remove, move } = useFieldArray({ control, name });
  const addId = `${baseId}-add`;
  // The id of the control focus should land on once the list has re-rendered.
  // Both edits move it: an append whose textarea is not focused makes a
  // keyboard user hunt for the box they just asked for, and a Remove that
  // leaves focus on a button that no longer exists drops it to <body>, which
  // puts the next Tab back at the top of the page.
  const focusTarget = useRef<string | null>(null);

  const sensors = useSensors(
    // 6px, matching the rules tree: below that, a click on a row control
    // registers as a drag.
    useSensor(PointerSensor, { activationConstraint: { distance: 6 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  );

  useEffect(() => {
    const id = focusTarget.current;
    if (id === null) return;
    focusTarget.current = null;
    document.getElementById(id)?.focus();
  }, [baseId, fields.length]);

  function handleRemove(index: number) {
    const next = fields.length - 1;
    // The row that slid into the removed one's place, or the row above when
    // the last one went; the Add button when the list empties (which only a
    // rollback can do — a plan keeps its floor of one).
    focusTarget.current = next === 0 ? addId : `${baseId}-action-${Math.min(index, next - 1)}`;
    remove(index);
  }

  function handleDragEnd(event: DragEndEvent) {
    const { active, over } = event;
    if (!over || active.id === over.id) return;
    const from = fields.findIndex((f) => f.id === active.id);
    const to = fields.findIndex((f) => f.id === over.id);
    if (from < 0 || to < 0) return;
    move(from, to);
  }

  const atCap = fields.length >= max;
  const listError = describeFieldError(`The ${heading.toLowerCase()} list`, errorAt(errors, name));

  return (
    <section className={styles.section}>
      <div className={styles.labelRow}>
        <h4 className={styles.subTitle} id={`${baseId}-heading`}>
          {heading}
        </h4>
        <span className={styles.hint}>{`${fields.length} / ${max}`}</span>
      </div>

      {fields.length === 0 ? (
        <p className={styles.hint}>{`No ${noun}s yet.`}</p>
      ) : (
        <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={handleDragEnd}>
          <SortableContext items={fields.map((f) => f.id)} strategy={verticalListSortingStrategy}>
            <ol className={own.list} aria-labelledby={`${baseId}-heading`}>
              {fields.map((field, index) => (
                <StepRow
                  key={field.id}
                  sortableId={field.id}
                  baseId={baseId}
                  control={control}
                  register={register}
                  setValue={setValue}
                  errors={errors}
                  name={name}
                  index={index}
                  total={fields.length}
                  noun={noun}
                  canRemove={fields.length > min}
                  removeDisabledHint={removeDisabledHint}
                  validateOnChange={validateOnChange}
                  withWhen={withWhen}
                  onMove={move}
                  onRemove={handleRemove}
                />
              ))}
            </ol>
          </SortableContext>
        </DndContext>
      )}

      {listError !== undefined ? (
        <p className={styles.error} role="alert">
          {listError}
        </p>
      ) : null}

      <Button
        id={addId}
        size="sm"
        variant="secondary"
        leadingIcon="plus"
        className={styles.addButton}
        disabled={atCap}
        {...(atCap ? { title: `A plan holds at most ${max} ${noun}s` } : {})}
        onClick={() => {
          focusTarget.current = `${baseId}-action-${fields.length}`;
          append(emptyAnalysisStep());
        }}
      >
        {addLabel}
      </Button>
      {atCap ? (
        <p className={styles.hint}>
          {`That is the limit of ${max} ${noun}s. Remove one to add another.`}
        </p>
      ) : null}
    </section>
  );
}

type StepRowProps = {
  sortableId: string;
  baseId: string;
  control: Control<AnalysisForm>;
  register: UseFormRegister<AnalysisForm>;
  setValue: UseFormSetValue<AnalysisForm>;
  errors: FieldErrors<AnalysisForm>;
  name: StepListName;
  index: number;
  total: number;
  noun: string;
  canRemove: boolean;
  removeDisabledHint: string;
  validateOnChange: boolean;
  withWhen: boolean;
  onMove: (from: number, to: number) => void;
  onRemove: (index: number) => void;
};

function StepRow(props: StepRowProps) {
  const {
    sortableId,
    baseId,
    control,
    register,
    setValue,
    errors,
    name,
    index,
    total,
    noun,
    canRemove,
    removeDisabledHint,
    validateOnChange,
    withWhen,
    onMove,
    onRemove,
  } = props;

  const { attributes, listeners, setNodeRef, setActivatorNodeRef, transform, transition } =
    useSortable({ id: sortableId });
  const position = index + 1;
  const actionId = `${baseId}-action-${index}`;
  const commandId = `${baseId}-command-${index}`;
  const riskId = `${baseId}-risk-${index}`;
  const whenId = `${baseId}-when-${index}`;

  const risk = useWatch({ control, name: stepPath(name, index, "risk") });
  const when = useWatch({ control, name: stepPath(name, index, "when") });
  const actionError = describeFieldError("Action", errorAt(errors, `${name}[${index}].action`));
  const commandError = describeFieldError("Command", errorAt(errors, `${name}[${index}].command`));
  const riskError = describeFieldError("Risk", errorAt(errors, `${name}[${index}].risk`));
  const whenError = describeFieldError("When", errorAt(errors, `${name}[${index}].when`));

  const commandField = register(stepPath(name, index, "command"));

  return (
    <li
      ref={setNodeRef}
      className={own.step}
      style={{ transform: CSS.Transform.toString(transform), transition: transition ?? undefined }}
    >
      <div className={own.head}>
        <IconButton
          ref={setActivatorNodeRef}
          icon="grip"
          label={`Drag to reorder ${noun} ${position}`}
          size="sm"
          className={own.handle}
          {...attributes}
          {...listeners}
        />
        <span className={own.position}>{position}</span>
        <div className={own.rowActions}>
          <IconButton
            icon="chevron-up"
            label={`Move ${noun} ${position} up`}
            size="sm"
            disabled={index === 0}
            onClick={() => onMove(index, index - 1)}
          />
          <IconButton
            icon="chevron-down"
            label={`Move ${noun} ${position} down`}
            size="sm"
            disabled={index === total - 1}
            onClick={() => onMove(index, index + 1)}
          />
          <IconButton
            icon="trash"
            label={`Remove ${noun} ${position}`}
            variant="ghostDanger"
            size="sm"
            disabled={!canRemove}
            {...(canRemove ? {} : { title: removeDisabledHint })}
            onClick={() => onRemove(index)}
          />
        </div>
      </div>

      <div className={styles.field}>
        <div className={styles.labelRow}>
          <label className={styles.label} htmlFor={actionId}>
            Action
          </label>
          <CharCounter
            control={control}
            name={stepPath(name, index, "action")}
            limit={ANALYSIS_LIMITS.action}
            id={`${actionId}-count`}
          />
        </div>
        <Textarea
          id={actionId}
          rows={2}
          placeholder="What an operator should do."
          aria-describedby={`${actionId}-count`}
          invalid={actionError !== undefined}
          errorMessage={actionError}
          {...register(stepPath(name, index, "action"))}
        />
      </div>

      <div className={own.grid}>
        <div className={own.controls}>
          <div className={styles.field}>
            <span className={styles.label} id={`${riskId}-label`}>
              Risk
            </span>
            <Select
              value={risk}
              onValueChange={(v) =>
                setValue(stepPath(name, index, "risk"), v as typeof risk, {
                  shouldDirty: true,
                  shouldValidate: validateOnChange,
                })
              }
            >
              <SelectTrigger
                id={riskId}
                placeholder="Pick a risk"
                aria-labelledby={`${riskId}-label ${riskId}`}
              />
              <SelectContent>
                {RISK_LEVELS.map((level) => (
                  <SelectItem key={level} value={level}>
                    {riskLabel(level)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            {riskError !== undefined ? (
              <p className={styles.error} role="alert">
                {riskError}
              </p>
            ) : null}
          </div>

          {withWhen ? (
            <div className={styles.field}>
              <span className={styles.label} id={`${whenId}-label`}>
                When
              </span>
              <Select
                value={when === "" || when === undefined ? WHEN_UNSET : when}
                onValueChange={(v) =>
                  setValue(
                    stepPath(name, index, "when"),
                    v === WHEN_UNSET ? "" : (v as typeof when),
                    { shouldDirty: true, shouldValidate: validateOnChange },
                  )
                }
              >
                <SelectTrigger id={whenId} aria-labelledby={`${whenId}-label ${whenId}`} />
                <SelectContent>
                  <SelectItem value={WHEN_UNSET}>—</SelectItem>
                  {STEP_WHENS.map((w) => (
                    <SelectItem key={w} value={w}>
                      {stepWhenLabel(w)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              {whenError !== undefined ? (
                <p className={styles.error} role="alert">
                  {whenError}
                </p>
              ) : null}
            </div>
          ) : null}
        </div>

        <div className={styles.field}>
          <div className={styles.labelRow}>
            <label className={styles.label} htmlFor={commandId}>
              Command
            </label>
            <CharCounter
              control={control}
              name={stepPath(name, index, "command")}
              limit={ANALYSIS_LIMITS.command}
              id={`${commandId}-count`}
            />
          </div>
          <Textarea
            id={commandId}
            rows={2}
            className={own.command}
            placeholder="Optional — the exact command to run."
            aria-describedby={`${commandId}-count`}
            invalid={commandError !== undefined}
            errorMessage={commandError}
            {...commandField}
            ref={(el) => {
              commandField.ref(el);
              autoGrow(el);
            }}
            onInput={(e) => autoGrow(e.currentTarget)}
          />
        </div>
      </div>
    </li>
  );
}
