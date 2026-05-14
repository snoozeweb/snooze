import type { ConditionType } from "./types";

export type ValueShape = "string" | "array" | "number" | "none";

type OperatorMeta = {
  type: Exclude<ConditionType, "AND" | "OR" | "NOT">;
  label: string;
  valueShape: ValueShape;
};

export const OPERATORS: OperatorMeta[] = [
  { type: "EQUALS", label: "equals", valueShape: "string" },
  { type: "CONTAINS", label: "contains", valueShape: "string" },
  { type: "MATCHES", label: "matches", valueShape: "string" },
  { type: "SEARCH", label: "search", valueShape: "string" },
  { type: "IN", label: "in", valueShape: "array" },
  { type: "LT", label: "<", valueShape: "number" },
  { type: "LE", label: "≤", valueShape: "number" },
  { type: "GT", label: ">", valueShape: "number" },
  { type: "GE", label: "≥", valueShape: "number" },
  { type: "EXISTS", label: "exists", valueShape: "none" },
  { type: "ALWAYS_TRUE", label: "always", valueShape: "none" },
];

const byType = new Map(OPERATORS.map((o) => [o.type, o]));

export function operatorLabel(type: ConditionType): string {
  if (type === "AND") return "all of";
  if (type === "OR") return "any of";
  if (type === "NOT") return "not";
  return byType.get(type)?.label ?? type.toLowerCase();
}

export function valueShapeForOp(type: ConditionType): ValueShape {
  if (type === "AND" || type === "OR" || type === "NOT") return "none";
  return byType.get(type)?.valueShape ?? "string";
}

export function defaultValueForOp(type: ConditionType): string | string[] | number | undefined {
  const shape = valueShapeForOp(type);
  if (shape === "string") return "";
  if (shape === "array") return [];
  if (shape === "number") return 0;
  return undefined;
}
