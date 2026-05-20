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
import type { Environment } from "@/features/admin/environments/types";
import styles from "./EnvironmentBar.module.css";

export type EnvironmentBarProps = {
  /** UIDs of currently selected environments. Empty / undefined = All. */
  selected: string[];
  onChange: (next: string[]) => void;
};

function isAdmin(): boolean {
  try {
    const raw = localStorage.getItem("permissions") || "[]";
    const perms = JSON.parse(raw) as unknown;
    if (!Array.isArray(perms)) return false;
    return perms.includes("rw_all") || perms.includes("rw_environment");
  } catch {
    return false;
  }
}

export function EnvironmentBar({ selected, onChange }: EnvironmentBarProps) {
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
        onClick={selectAll}
      >
        All
      </button>
      {envs.map((env) => {
        const active = env.uid !== undefined && selectedSet.has(env.uid);
        const color = env.color || "var(--accent)";
        return (
          <button
            key={env.uid ?? env.name}
            type="button"
            className={styles.pill}
            data-state={active ? "active" : "inactive"}
            style={
              active
                ? { background: color, borderColor: color, color: "#fff" }
                : { borderColor: color, color }
            }
            onClick={() => toggle(env.uid)}
            title={env.comment ?? env.name}
          >
            {env.name}
          </button>
        );
      })}
      {isAdmin() ? (
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
