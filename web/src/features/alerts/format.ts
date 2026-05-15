import type { BadgeVariant } from "@/shared/ui/Badge";
import type { AlertSeverity, AlertState } from "./types";

const SEVERITY_MAP: Record<string, BadgeVariant> = {
  critical: "critical",
  error: "error",
  warning: "warning",
  info: "info",
};

export function severityBadgeVariant(severity: AlertSeverity): BadgeVariant {
  return SEVERITY_MAP[severity.toLowerCase()] ?? "muted";
}

const STATE_LABEL: Record<AlertState, string> = {
  // Freshly-ingested records carry an empty state until a comment moves them
  // to ack/close/esc. Display them as Open so the column reads cleanly.
  "": "Open",
  open: "Open",
  ack: "Acknowledged",
  close: "Closed",
  shelved: "Shelved",
};

const STATE_VARIANT: Record<AlertState, BadgeVariant> = {
  "": "neutral",
  open: "neutral",
  ack: "info",
  close: "muted",
  shelved: "muted",
};

export function stateLabel(state: AlertState): string {
  return STATE_LABEL[state] ?? state;
}

export function stateBadgeVariant(state: AlertState): BadgeVariant {
  return STATE_VARIANT[state] ?? "neutral";
}

export function formatRelativeTime(dateEpochSec: number | undefined): string {
  if (dateEpochSec === undefined || dateEpochSec === 0) return "—";
  const now = Math.floor(Date.now() / 1000);
  const diff = now - dateEpochSec;
  if (diff < 60) return diff <= 1 ? "just now" : `${diff}s`;
  if (diff < 3600) return `${Math.floor(diff / 60)}m`;
  if (diff < 86400) return `${Math.floor(diff / 3600)}h`;
  return `${Math.floor(diff / 86400)}d`;
}
