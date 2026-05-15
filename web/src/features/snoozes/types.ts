import type { Condition } from "@/lib/condition/types";
import type { TimeConstraintsGroup } from "@/lib/timeconstraints/types";

export type Snooze = {
  uid?: string;
  name: string;
  comment?: string;
  enabled?: boolean;
  condition?: Condition;
  // Active time windows (weekdays, daily slots, absolute date ranges).
  // The snooze only matches when the current moment is within every
  // populated family — see internal/timeconstraints/timeconstraints.go.
  // Expiration (auto-disable past a date) is expressed by setting
  // `time_constraints.datetime[0].until` to a past timestamp; the
  // Active / Upcoming / Expired tabs read the same field.
  time_constraints?: TimeConstraintsGroup;
  // discard:true drops matching alerts from the pipeline; default (false)
  // tags them snoozed but lets them through.
  discard?: boolean;
  // Server-maintained match counter. Read-only on the wire.
  hits?: number;
  // Server-set creator (the user that issued the snooze).
  name_create?: string;
};
