import type { BadgeVariant } from "@/shared/ui/Badge";
import type { AuditAction } from "./types";

export const ACTION_LABEL: Record<AuditAction, string> = {
  create: "created",
  patch: "edited",
  replace: "replaced",
  delete: "deleted",
  login: "login",
  login_failed: "login failed",
  refresh: "token refresh",
  logout: "logout",
};

export const ACTION_VARIANT: Record<AuditAction, BadgeVariant> = {
  create: "info",
  patch: "neutral",
  replace: "warning",
  delete: "muted",
  login: "ok",
  login_failed: "critical",
  refresh: "neutral",
  logout: "muted",
};
