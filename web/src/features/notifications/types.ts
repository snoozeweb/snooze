import type { Condition } from "@/lib/condition/types";
import type { TimeConstraintsGroup } from "@/lib/timeconstraints/types";

// Frequency throttles repeated notifications. Mirrors internal/pluginimpl/
// notification.Frequency: deliver at most `total` notifications, spaced by
// `every` seconds, with an initial `delay`.
export type Frequency = {
  total?: number;
  delay?: number;
  every?: number;
};

export type Notification = {
  uid?: string;
  name: string;
  comment?: string;
  enabled?: boolean;
  condition?: Condition;
  actions?: string[];
  time_constraints?: TimeConstraintsGroup;
  frequency?: Frequency;
};

export type Action = {
  uid?: string;
  name: string;
  comment?: string;
  action_type?: string;
  action?: Record<string, unknown>;
};
