import type { Condition } from "@/lib/condition/types";

export type Snooze = {
  uid?: string;
  name: string;
  comment?: string;
  enabled?: boolean;
  condition?: Condition;
  ttl?: number;
};
