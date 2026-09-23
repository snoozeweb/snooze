// OwnerFilter — the row of faces beside the lifecycle tabs that narrows the
// list to what given people own (or to what nobody owns).
//
// Order is fixed so muscle memory works: the signed-in operator first, always
// — greyed at zero, because "I own nothing here" is an answer too — then
// everyone else who owns at least one alert in the current tab ∧ search ∧
// environment (the server's order: most first), then the Unowned chip. A
// handful stay inline; the rest fold behind "+N", a popover with a search box.
//
// The counts come from GET /record/owners over that same tab ∧ search ∧ env
// condition WITHOUT the owner filter itself, so selecting Alice does not make
// Bob's count collapse to zero — each chip always says what clicking it (on
// its own) would show.
import { useMemo, useState } from "react";
import { Avatar } from "@/shared/ui/Avatar";
import { Input } from "@/shared/ui/Input";
import { Popover, PopoverContent, PopoverTrigger } from "@/shared/ui/Popover";
import { Tooltip } from "@/shared/ui/Tooltip";
import { useAuth } from "@/lib/auth/store";
import { personLabel, usePeople, findPerson, type Person } from "@/shared/people/api";
import { useRecordOwners } from "./api";
import { UNOWNED_NOUN } from "./lifecycle";
import { toggleOwner, UNOWNED_TOKEN } from "./ownerFilter";
import styles from "./OwnerFilter.module.css";

/** Person chips shown inline (the signed-in operator included) before "+N". */
export const OWNER_CHIPS_INLINE = 6;

export type OwnerFilterProps = {
  /** Selected tokens: logins, plus `~none` for Unowned. */
  value: readonly string[];
  onChange: (next: string[]) => void;
  /** The list's condition minus the owner filter, base64url — what the counts
   *  are counted over. Undefined counts every record (the All tab, no search). */
  countsQ: string | undefined;
  /** Poll the counts on the list's own cadence, so a chip does not lag the
   *  table it filters. */
  refetchIntervalMs?: number | undefined;
};

type Chip = { name: string; method: string; count: number; label: string; isMe: boolean };

function alertsWord(n: number): string {
  return `${n} alert${n === 1 ? "" : "s"}`;
}

export function OwnerFilter({ value, onChange, countsQ, refetchIntervalMs }: OwnerFilterProps) {
  const { claims } = useAuth();
  const me = claims?.sub ?? "";
  const myMethod = typeof claims?.method === "string" ? claims.method : "";
  const counts = useRecordOwners(
    countsQ,
    refetchIntervalMs !== undefined ? { refetchInterval: refetchIntervalMs } : undefined,
  );
  const people = usePeople();
  const loaded = counts.data !== undefined;

  const chips = useMemo<Chip[]>(() => {
    const byOwner = new Map((counts.data?.data ?? []).map((c) => [c.owner, c.count]));
    const make = (name: string, method: string | undefined, isMe: boolean): Chip => {
      const person: Person | undefined = findPerson(people.data, name, method);
      return {
        name,
        method: method || person?.method || "",
        count: byOwner.get(name) ?? 0,
        label: personLabel(person, name),
        isMe,
      };
    };
    const out: Chip[] = [];
    if (me !== "") out.push(make(me, myMethod || undefined, true));
    for (const c of counts.data?.data ?? []) {
      if (c.owner === "" || c.owner === me || c.count <= 0) continue;
      out.push(make(c.owner, undefined, false));
    }
    // A selected owner who has nothing in this tab still gets a chip —
    // otherwise the only way to deselect them would be the chip strip below
    // the table, and the list would be filtered by someone who is not shown.
    for (const token of value) {
      if (token === UNOWNED_TOKEN || out.some((c) => c.name === token)) continue;
      out.push(make(token, undefined, false));
    }
    return out;
  }, [counts.data, people.data, me, myMethod, value]);

  // Inline: the first few, plus any selected chip that would otherwise be
  // folded away — a pressed toggle hidden in a popover is a filter nobody can
  // see is on.
  const inline = chips.filter((c, i) => i < OWNER_CHIPS_INLINE || value.includes(c.name));
  const overflow = chips.filter((c) => !inline.includes(c));
  const unownedCount = counts.data?.unowned ?? 0;
  const unownedPressed = value.includes(UNOWNED_TOKEN);

  const toggle = (token: string) => onChange(toggleOwner(value, token));

  return (
    <div className={styles.strip} role="group" aria-label="Filter by owner">
      {inline.map((c) => (
        <PersonChip
          key={c.name}
          chip={c}
          loaded={loaded}
          pressed={value.includes(c.name)}
          onToggle={() => toggle(c.name)}
        />
      ))}
      {overflow.length > 0 ? (
        <OverflowPicker chips={overflow} value={value} loaded={loaded} onToggle={toggle} />
      ) : null}
      <button
        type="button"
        className={styles.chip}
        data-kind="unowned"
        data-state={unownedPressed ? "active" : "inactive"}
        data-empty={loaded && unownedCount === 0 ? "true" : undefined}
        aria-pressed={unownedPressed}
        aria-label={loaded ? `${UNOWNED_NOUN}, ${alertsWord(unownedCount)}` : UNOWNED_NOUN}
        onClick={() => toggle(UNOWNED_TOKEN)}
      >
        <span className={styles.unownedLabel}>{UNOWNED_NOUN}</span>
        {loaded ? <span className={styles.count}>{unownedCount}</span> : null}
      </button>
    </div>
  );
}

function chipName(c: Chip): string {
  return c.isMe ? `${c.label} (you)` : c.label;
}

function PersonChip({
  chip,
  loaded,
  pressed,
  onToggle,
}: {
  chip: Chip;
  loaded: boolean;
  pressed: boolean;
  onToggle: () => void;
}) {
  const name = chipName(chip);
  return (
    <Tooltip content={loaded ? `${name} · ${alertsWord(chip.count)}` : name}>
      <button
        type="button"
        className={styles.chip}
        data-state={pressed ? "active" : "inactive"}
        data-me={chip.isMe ? "true" : undefined}
        data-empty={loaded && chip.count === 0 ? "true" : undefined}
        aria-pressed={pressed}
        aria-label={loaded ? `${name}, ${alertsWord(chip.count)}` : name}
        onClick={onToggle}
      >
        <Avatar name={chip.name} method={chip.method || undefined} size="sm" decorative />
        {loaded ? <span className={styles.count}>{chip.count}</span> : null}
      </button>
    </Tooltip>
  );
}

function OverflowPicker({
  chips,
  value,
  loaded,
  onToggle,
}: {
  chips: Chip[];
  value: readonly string[];
  loaded: boolean;
  onToggle: (token: string) => void;
}) {
  const [query, setQuery] = useState("");
  const q = query.trim().toLowerCase();
  const shown = q
    ? chips.filter((c) => c.name.toLowerCase().includes(q) || c.label.toLowerCase().includes(q))
    : chips;
  return (
    <Popover
      onOpenChange={(open) => {
        if (!open) setQuery("");
      }}
    >
      <PopoverTrigger
        className={styles.chip}
        data-kind="more"
        aria-label={`${chips.length} more owner${chips.length === 1 ? "" : "s"}`}
      >
        +{chips.length}
      </PopoverTrigger>
      <PopoverContent align="end" className={styles.popover!}>
        <Input
          size="sm"
          leadingIcon="search"
          aria-label="Search owners"
          placeholder="Search owners…"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
        />
        {shown.length === 0 ? (
          <p className={styles.noMatch}>No owner matches “{query.trim()}”.</p>
        ) : (
          <div className={styles.list} role="group" aria-label="More owners">
            {shown.map((c) => {
              const pressed = value.includes(c.name);
              return (
                <button
                  key={c.name}
                  type="button"
                  className={styles.listItem}
                  data-state={pressed ? "active" : "inactive"}
                  aria-pressed={pressed}
                  aria-label={loaded ? `${chipName(c)}, ${alertsWord(c.count)}` : chipName(c)}
                  onClick={() => onToggle(c.name)}
                >
                  <Avatar name={c.name} method={c.method || undefined} size="sm" decorative />
                  <span className={styles.listName}>{chipName(c)}</span>
                  {loaded ? <span className={styles.count}>{c.count}</span> : null}
                </button>
              );
            })}
          </div>
        )}
      </PopoverContent>
    </Popover>
  );
}
