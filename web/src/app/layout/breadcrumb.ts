import { NAV_ITEMS, GROUP_LABELS } from "./nav-items";
import type { useMatches } from "@tanstack/react-router";

export type Breadcrumb = {
  group?: string;
  label: string;
};

export function pickBreadcrumb(matches: ReturnType<typeof useMatches>): Breadcrumb | undefined {
  const last = matches[matches.length - 1];
  if (!last) return undefined;
  const path = last.pathname;
  const navItem = NAV_ITEMS.find((i) => path === i.to || path.startsWith(i.to + "/"));
  if (!navItem) return undefined;
  return {
    group: GROUP_LABELS[navItem.group],
    label: navItem.label,
  };
}
