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

// ActionEnvelope mirrors the {selected, subcontent} pair the backend stores at
// `action.action`. `selected` is the registry key of the notifier plugin
// (mail / webhook / script / …); `subcontent` is the action_form payload the
// notifier consumes via NotificationPayload.Meta. See
// internal/pluginimpl/notification/plugin.go:actionEnvelope and the
// Python-era plugins/core/action layout this replaces.
export type ActionEnvelope = {
  selected?: string;
  subcontent?: Record<string, unknown>;
};

export type Action = {
  uid?: string;
  name: string;
  comment?: string;
  action?: ActionEnvelope;
};
