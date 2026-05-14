export type LeafStringOp = "EQUALS" | "CONTAINS" | "MATCHES" | "SEARCH";
export type LeafNumberOp = "LT" | "GT" | "LE" | "GE";
export type LeafArrayOp = "IN";
export type LeafExistsOp = "EXISTS";
export type LeafConstOp = "ALWAYS_TRUE";
export type GroupOp = "AND" | "OR";
export type NotOp = "NOT";

export type Condition =
  | { type: LeafConstOp }
  | { type: LeafStringOp; field: string; value: string }
  | { type: LeafArrayOp; field: string; value: string[] }
  | { type: LeafNumberOp; field: string; value: number }
  | { type: LeafExistsOp; field: string }
  | { type: NotOp; arg: Condition }
  | { type: GroupOp; args: Condition[] };

export type ConditionType = Condition["type"];

export type ConditionPath = ReadonlyArray<number>;
