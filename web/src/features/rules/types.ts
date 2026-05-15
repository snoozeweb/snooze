import type { Condition } from "@/lib/condition/types";

export type Rule = {
  uid?: string;
  name: string;
  comment?: string;
  enabled?: boolean;
  condition?: Condition;
  // Wire shape is the positional [op, field, …args] form used by the
  // Python era and the Go-internal modification.Modification. The React
  // editor de/serialises this via shared/modifications/wire.ts.
  modifications?: unknown[][];
};

export type AggregateRule = Rule & {
  fields?: string[];
  watch?: string[];
  throttle?: number;
};
