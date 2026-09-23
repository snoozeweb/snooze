// The owner filter's URL and query semantics, kept free of React so the round
// trip and the condition it builds are unit-testable on their own.
//
// URL: `?owner=alice,bob,~none` — logins, comma-separated, with the reserved
// `~none` standing for "Unowned". A `~` cannot start a login the auth backends
// hand out, so the token cannot collide with a person.
//
// Query: an OR over the selection (multi-select widens, it never narrows),
// ANDed by AlertsPage with the tab, search and environment conditions.
import type { Condition } from "@/lib/condition/types";

/** The URL token for the "Unowned" chip. */
export const UNOWNED_TOKEN = "~none";

/** `?owner=` → the selected tokens, in URL order, without blanks or repeats. */
export function parseOwnerParam(raw: string | undefined): string[] {
  if (!raw) return [];
  const out: string[] = [];
  for (const part of raw.split(",")) {
    const token = part.trim();
    if (token !== "" && !out.includes(token)) out.push(token);
  }
  return out;
}

/** The selection → `?owner=`, or undefined (drop the key) when empty. */
export function formatOwnerParam(owners: readonly string[]): string | undefined {
  const clean = parseOwnerParam(owners.join(","));
  return clean.length > 0 ? clean.join(",") : undefined;
}

/** Adds `token` to the selection, or takes it out if it is already in. */
export function toggleOwner(owners: readonly string[], token: string): string[] {
  return owners.includes(token) ? owners.filter((o) => o !== token) : [...owners, token];
}

/**
 * The records the selection matches, or null for no selection.
 *
 *   owner = alice OR owner = bob OR NOT EXISTS owner OR owner = ""
 *
 * "Unowned" is two clauses because clearing ownership writes `""` rather than
 * unsetting the key (the pipeline's merge write could not unset it), while a
 * record that predates ownership has no key at all. A record carrying only a
 * previous owner matches it too — nobody is on it now.
 */
export function ownerCondition(owners: readonly string[]): Condition | null {
  const args: Condition[] = [];
  for (const token of owners) {
    if (token === UNOWNED_TOKEN) {
      args.push(
        { type: "NOT", arg: { type: "EXISTS", field: "owner" } },
        { type: "EQUALS", field: "owner", value: "" },
      );
    } else {
      args.push({ type: "EQUALS", field: "owner", value: token });
    }
  }
  if (args.length === 0) return null;
  return args.length === 1 ? args[0]! : { type: "OR", args };
}
