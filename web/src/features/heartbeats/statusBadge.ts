import type { BadgeVariant } from "@/shared/ui/Badge";
import type { HeartbeatStatus } from "./types";

export const STATUS_BADGE: Record<HeartbeatStatus, BadgeVariant> = {
  ok: "ok",
  slow: "warning",
  overdue: "error",
};
