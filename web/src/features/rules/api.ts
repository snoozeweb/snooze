import { defineResource } from "@/lib/api/resource";
import type { Rule, AggregateRule } from "./types";

export const Rules = defineResource<Rule>("rule");
export const AggregateRules = defineResource<AggregateRule>("aggregaterule");
