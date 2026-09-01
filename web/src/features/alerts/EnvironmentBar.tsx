// EnvironmentBar — horizontal button group at the top of the alerts page.
// One button per environment plus an "All" toggle. Selecting environments
// filters the alerts list by OR'ing each environment's `condition`. The
// resulting clause is AND'd with the lifecycle tab and the DSL search by
// AlertsPage.buildQueryParam.
//
// Mirrors the old Python 1.x web UI's Environment.vue / "Environment bar"
// affordance.
import { useMemo } from "react";
import { Link } from "@tanstack/react-router";
import { Icon } from "@/shared/icons/Icon";
import { Environments } from "@/features/admin/environments/api";
import { envColor } from "@/features/admin/environments/environment-color";
import type { Environment } from "@/features/admin/environments/types";
import { useAuth } from "@/lib/auth/store";
import { hasAnyPermission } from "@/lib/auth/permissions";
import styles from "./EnvironmentBar.module.css";

export type EnvironmentBarProps = {
  /** UIDs of currently selected environments. Empty / undefined = All. */
  selected: string[];
  onChange: (next: string[]) => void;
};

/**
 * readableTextOn picks a readable foreground (near-black vs near-white) for
 * text sitting on a solid `bg` fill. Active env pills paint the environment's
 * own colour as the background, so a hardcoded `#fff` text failed contrast on
 * light env colours (e.g. a yellow or pale-blue environment). We compute
 * contrast off the bg's relative luminance using WCAG-style coefficients,
 * the same as the severity-color luminance test uses.
 *
 * Returns theme-independent ink tokens (--ink-dark / --ink-light) because the
 * background is arbitrary user data that never changes with the theme. Non-
 * #rrggbb inputs (CSS vars, named colours) fall back to the strong text token.
 */
function readableTextOn(bg: string): string {
  const m = /^#([0-9a-f]{6})$/i.exec(bg.trim());
  if (!m) return "var(--text-strong)";
  const hex = m[1]!;
  const r = parseInt(hex.slice(0, 2), 16);
  const g = parseInt(hex.slice(2, 4), 16);
  const b = parseInt(hex.slice(4, 6), 16);
  const lum = 0.2126 * r + 0.7152 * g + 0.0722 * b;
  // Threshold ~140/255: above → dark ink, below → light ink.
  return lum > 140 ? "var(--ink-dark)" : "var(--ink-light)";
}

const ENV_MANAGE_PERMS = ["rw_all", "rw_environment"] as const;

export function EnvironmentBar({ selected, onChange }: EnvironmentBarProps) {
  const { claims } = useAuth();
  const canManage = hasAnyPermission(claims, ENV_MANAGE_PERMS);
  const list = Environments.useList({
    limit: 200,
    orderby: "tree_order",
    asc: true,
  });

  const envs = useMemo<Environment[]>(() => list.data?.data ?? [], [list.data]);
  const selectedSet = useMemo(() => new Set(selected), [selected]);
  const allActive = selected.length === 0;

  if (!list.isPending && envs.length === 0) {
    // Nothing to render — keep the chrome clean when no environments exist.
    return null;
  }

  function toggle(uid: string | undefined) {
    if (!uid) return;
    const next = new Set(selectedSet);
    if (next.has(uid)) next.delete(uid);
    else next.add(uid);
    onChange(Array.from(next));
  }

  function selectAll() {
    onChange([]);
  }

  return (
    <div role="group" aria-label="Filter by environment" className={styles.bar}>
      <button
        type="button"
        className={styles.pill}
        data-state={allActive ? "active" : "inactive"}
        aria-pressed={allActive}
        onClick={selectAll}
      >
        All
      </button>
      {envs.map((env) => {
        const active = env.uid !== undefined && selectedSet.has(env.uid);
        // Fall back to a stable per-name palette colour (not a uniform accent or
        // a black pill) so uncoloured environments stay distinguishable.
        const color = env.color || envColor(env.name);
        return (
          <button
            key={env.uid ?? env.name}
            type="button"
            className={styles.pill}
            data-state={active ? "active" : "inactive"}
            aria-pressed={active}
            style={
              active
                ? { background: color, borderColor: color, color: readableTextOn(color) }
                : undefined
            }
            onClick={() => toggle(env.uid)}
            title={env.comment ?? env.name}
          >
            {/* Unselected pills carry the environment's colour as a swatch and
                keep neutral ink. Painting the name itself in the environment
                colour meant an operator who coloured `production` red got a
                filter chip indistinguishable from a critical-severity badge —
                the colour is an identifier, not a status. */}
            {active ? null : (
              <span className={styles.swatch} style={{ background: color }} aria-hidden="true" />
            )}
            {env.name}
          </button>
        );
      })}
      {canManage ? (
        <Link
          to="/web/admin/environments"
          className={styles.cog}
          aria-label="Manage environments"
          title="Manage environments"
        >
          <Icon name="settings" size={14} />
        </Link>
      ) : null}
    </div>
  );
}
