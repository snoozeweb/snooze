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
  // Tree shape: a rule with a non-empty `parents` list is a child of those
  // rules and only evaluated when at least one parent matches (see
  // internal/pluginimpl/rule/plugin.go processRules). Sibling order is
  // controlled by `tree_order` (ascending).
  parents?: string[];
  tree_order?: number;
};

export type AggregateRule = Rule & {
  fields?: string[];
  watch?: string[];
  throttle?: number;
};
