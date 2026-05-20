import type { Condition } from "@/lib/condition/types";

export type Environment = {
  uid?: string;
  name: string;
  // Filter condition used to narrow the alerts list when this environment
  // is active. Selecting an environment AND's its condition with the
  // active lifecycle tab and any DSL search.
  condition?: Condition;
  color?: string;
  comment?: string;
  // Ordering rank for the environment bar on the alerts page. Lower
  // tree_order renders first.
  tree_order?: number;
};
