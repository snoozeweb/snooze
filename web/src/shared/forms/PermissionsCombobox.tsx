// PermissionsCombobox — the shared RBAC permission picker used by the role
// editor and the API-key mint form. It wraps MultiCombobox with two pieces of
// permission-specific behaviour so both call sites stay in sync:
//
//   1. each option carries a human description (permissionDescription) shown as
//      muted secondary text, so an operator sees what a grant does rather than
//      decoding the ro_/rw_/can_ naming convention; and
//   2. any already-selected value missing from `available` is appended as its
//      own option, so a legacy grant (role editor) or a custom-typed string
//      (API-key form, allowCustom) still renders as a badge and survives a save
//      round-trip.
//
// The catalogue itself is NOT fetched here: callers pass `available` already
// scoped to their context — the role editor passes the full server catalogue,
// the API-key form passes the caller's own permissions (the subset-of-caller
// gate) minus the reserved tenant grants.
import { useMemo } from "react";
import { MultiCombobox, type MultiComboboxOption } from "@/shared/ui/MultiCombobox";
import { permissionDescription } from "@/lib/format/permission-info";

export type PermissionsComboboxProps = {
  /** Currently selected permission strings. */
  value: string[];
  onChange: (next: string[]) => void;
  /** The assignable permission catalogue this picker offers, scoped by the caller. */
  available: string[];
  /** Allow typing permission strings not present in `available` (free-form). */
  allowCustom?: boolean;
  placeholder?: string;
  "aria-label"?: string;
};

export function PermissionsCombobox({
  value,
  onChange,
  available,
  allowCustom,
  placeholder = "Select one or more permissions",
  "aria-label": ariaLabel = "Permissions",
}: PermissionsComboboxProps) {
  const options = useMemo(() => {
    const toOption = (p: string): MultiComboboxOption => {
      const description = permissionDescription(p);
      return description ? { value: p, label: p, description } : { value: p, label: p };
    };
    const seen = new Set(available);
    const merged = available.map(toOption);
    for (const p of value) {
      if (!seen.has(p)) {
        merged.push(toOption(p));
        seen.add(p);
      }
    }
    return merged;
  }, [available, value]);

  return (
    <MultiCombobox
      aria-label={ariaLabel}
      placeholder={placeholder}
      options={options}
      value={value}
      onChange={onChange}
      allowCustom={allowCustom ?? false}
    />
  );
}
