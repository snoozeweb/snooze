import type { Condition } from "@/lib/condition/types";
import type { Modification } from "@/shared/modifications/types";

export type Rule = {
  uid?: string;
  name: string;
  comment?: string;
  enabled?: boolean;
  condition?: Condition;
  modifications?: Modification[];
};

export type AggregateRule = Rule & {
  fields?: string[];
  watch?: string[];
  throttle?: number;
};
