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

// Reject rule: a flat, terminal first-match policy evaluated at ingest by the
// `reject` processor (before the rule tree). A strict subset of Rule — no
// modifications / parents / tree_order. Matching alerts are aborted with HTTP
// 422 to the sender.
export type RejectRule = {
  uid?: string;
  name: string;
  enabled?: boolean;
  condition?: Condition;
};

export type AggregateRule = Rule & {
  fields?: string[];
  watch?: string[];
  // Scalar seconds (applies always) or a { value: seconds, …, default: seconds }
  // map matched against the rule's `watch` field values (first match wins).
  throttle?: number | Record<string, number>;
};
