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
  // Server-stamped delivery counters (read-only). `hits` counts successful
  // deliveries, `last_sent` is the epoch (seconds) of the most recent one.
  // Both are maintained by the dispatcher via a read-modify-write on
  // successful deliveries; client-supplied values are stripped on write, so
  // the editor must never echo them back (see NotificationEditor).
  hits?: number;
  last_sent?: number;
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
