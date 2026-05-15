// Wire shape mirroring internal/timeconstraints/timeconstraints.go.
// Each family is a list of constraints OR'd within the list; populated
// families are AND'd together. An empty Group always matches.

export type DateTimeConstraint = {
  // RFC3339 strings (or null/missing for a half-open ray).
  from?: string;
  until?: string;
};

export type TimeOfDayConstraint = {
  // "HH:MM", "HH:MM:SS", or with an offset "HH:MM±HH:MM".
  from?: string;
  until?: string;
};

export type WeekdaysConstraint = {
  // 0 = Sunday, 6 = Saturday (matches Python's strftime("%w")).
  weekdays: number[];
};

export type TimeConstraintsGroup = {
  datetime?: DateTimeConstraint[];
  time?: TimeOfDayConstraint[];
  weekdays?: WeekdaysConstraint[];
};

export const WEEKDAY_LABELS = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"] as const;
