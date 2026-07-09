// UsersMultiSelect — pick zero or more existing users from a single dropdown,
// so an operator never has to hand-type a username or guess which auth backend
// (local / ldap / an OIDC method) owns it. It mirrors PermissionsCombobox: a
// MultiCombobox whose options carry a muted secondary line — here the user's
// auth method — the same treatment the role editor uses for permissions.
//
// A "user" on the wire is a {username, method} pair (Group.members, the RBAC
// principal). This component works in those terms: `value` and `onChange` speak
// UserRef[], and the string encoding MultiCombobox needs is an internal detail.
//
// The catalogue is fetched here (the user list) rather than passed in, matching
// the shared/ components that already reach into feature APIs (Logo,
// RowDetailPanel, ConditionPreview). Any selected pair missing from the fetched
// list — e.g. an LDAP user who has never logged in, so was never provisioned —
// is appended as its own option so it still renders and survives a save.
import { useMemo } from "react";
import { MultiCombobox, type MultiComboboxOption } from "@/shared/ui/MultiCombobox";
import { Users } from "@/features/admin/users/api";

/** A user principal: a username scoped by the auth backend that owns it. */
export type UserRef = { username: string; method: string };

// MultiCombobox keys on a single string, but a user is a (method, username)
// pair — "alice" in local and "alice" in ldap are different principals. We pack
// both into one value with a NUL separator (never present in a username or a
// method name) so the pair round-trips losslessly.
const SEP = "\u0000";
const encode = (u: UserRef) => `${u.method}${SEP}${u.username}`;
const decode = (key: string): UserRef => {
  const i = key.indexOf(SEP);
  if (i === -1) return { username: key, method: "local" };
  return { method: key.slice(0, i), username: key.slice(i + 1) };
};

export type UsersMultiSelectProps = {
  value: UserRef[];
  onChange: (next: UserRef[]) => void;
  placeholder?: string;
  "aria-label"?: string;
};

export function UsersMultiSelect({
  value,
  onChange,
  placeholder,
  "aria-label": ariaLabel = "Members",
}: UsersMultiSelectProps) {
  // limit: 500 matches the other admin lookups (UsersPage's role index); the
  // combobox filters client-side, so this is the practical ceiling before we'd
  // need server-side search.
  const users = Users.useList({ limit: 500, orderby: "name", asc: true });

  const selectedKeys = useMemo(() => value.map(encode), [value]);

  const options = useMemo<MultiComboboxOption[]>(() => {
    const seen = new Set<string>();
    const opts: MultiComboboxOption[] = [];
    const push = (u: UserRef) => {
      const key = encode(u);
      if (seen.has(key)) return;
      seen.add(key);
      opts.push({ value: key, label: u.username, description: u.method });
    };
    for (const u of users.data?.data ?? []) {
      push({ username: u.name, method: u.method ?? u.type ?? "local" });
    }
    // Keep already-selected principals selectable even if they aren't in the
    // fetched page/list, so removing them stays possible and a save is lossless.
    for (const u of value) push(u);
    return opts;
  }, [users.data, value]);

  return (
    <MultiCombobox
      aria-label={ariaLabel}
      placeholder={placeholder ?? (users.isPending ? "Loading users…" : "Select users…")}
      noResultsLabel="No matching users"
      options={options}
      value={selectedKeys}
      onChange={(next) => onChange(next.map(decode))}
    />
  );
}
