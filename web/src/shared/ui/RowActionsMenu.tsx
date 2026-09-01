import type { IconName } from "@/shared/icons/icon-names";
import { IconButton } from "./IconButton";
import { Menu, MenuContent, MenuItem, MenuSeparator, MenuTrigger } from "./Menu";
import styles from "./RowActionsMenu.module.css";

export type RowActionItem = {
  key: string;
  label: string;
  icon?: IconName;
  danger?: boolean;
  disabled?: boolean;
  /** Render this action as a labelled Button (icon+text) rather than an
   *  icon-only control, in the one place that has room for it today: the
   *  detail-drawer header's quick actions (see DataTable's `detailsActions`).
   *  Icon-only renderings (row hover, kebab already shows text) ignore it. */
  emphasize?: boolean;
  onSelect: () => void;
};

/** A visual divider between logical groups of kebab items — e.g. lifecycle
 *  actions vs. silence-and-suppress actions. Only meaningful inside a
 *  `rowActions` (kebab) list; row-level quick-action bars never contain one. */
export type RowActionSeparator = { key: string; separator: true };

export type RowAction = RowActionItem | RowActionSeparator;

/** Narrows a RowAction to its actionable variant — used by every renderer
 *  that doesn't understand separators (row hover buttons, the drawer
 *  header's quick actions). */
export function isRowActionItem(a: RowAction): a is RowActionItem {
  return !("separator" in a);
}

/** The row-actions kebab: a Menu trigger (⋯) that opens a dropdown of
 *  per-row actions. Shared by DataTable and any bespoke row surface (e.g.
 *  RulesTreeTable) that wants the same discoverable affordance instead of
 *  right-click-only actions. */
export function RowActionsMenu({
  actions,
  badge,
}: {
  actions: RowAction[];
  badge?: { count: number; label?: string } | undefined;
}) {
  const showBadge = !!badge && badge.count > 0;
  // Fold the badge meaning into the trigger's accessible name so the pill
  // isn't a sighted-only signal. Radix MenuTrigger is asChild → the IconButton
  // must stay its direct child, so the pill is an absolutely-positioned
  // sibling (pointer-events:none) anchored by the relative wrapper.
  const triggerLabel = showBadge && badge?.label ? `Row actions, ${badge.label}` : "Row actions";
  const menu = (
    <Menu>
      <MenuTrigger>
        {/* Radix MenuTrigger child → opt out of IconButton's own Tooltip: the
            kebab already reads as an actions affordance and its aria-label
            names it; a hover tooltip repeating "Row actions" is just noise. */}
        <IconButton icon="more-horizontal" label={triggerLabel} size="sm" withTooltip={false} />
      </MenuTrigger>
      <MenuContent>
        {actions.map((a) =>
          isRowActionItem(a) ? (
            <MenuItem
              key={a.key}
              {...(a.icon ? { leadingIcon: a.icon } : {})}
              {...(a.danger ? { danger: true } : {})}
              {...(a.disabled ? { disabled: true } : {})}
              onSelect={a.onSelect}
            >
              {a.label}
            </MenuItem>
          ) : (
            <MenuSeparator key={a.key} />
          ),
        )}
      </MenuContent>
    </Menu>
  );
  if (!showBadge) return menu;
  return (
    <span className={styles.actionsBadgeWrap}>
      {menu}
      <span className={styles.actionsBadge} aria-hidden="true">
        {badge.count > 99 ? "99+" : badge.count}
      </span>
    </span>
  );
}
