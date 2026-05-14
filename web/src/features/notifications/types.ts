import type { Condition } from "@/lib/condition/types";

export type Notification = {
  uid?: string;
  name: string;
  comment?: string;
  enabled?: boolean;
  condition?: Condition;
  actions?: string[];
};

export type Action = {
  uid?: string;
  name: string;
  comment?: string;
  action_type?: string;
  action?: Record<string, unknown>;
};
